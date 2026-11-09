package analysis

import (
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/ptbr"
)

// Verdict says where the day's problems most likely came from.
type Verdict string

// Verdicts.
const (
	VerdictNone     Verdict = "none"     // no problem found
	VerdictLocal    Verdict = "local"    // Wi-Fi or router
	VerdictProvider Verdict = "provider" // beyond the router
	VerdictCapacity Verdict = "capacity" // responsive, but slow
	VerdictUnknown  Verdict = "unknown"  // not enough information
)

// Diagnosis compares the path to the router with the path to the internet
// during the worst hours.
type Diagnosis struct {
	Verdict       Verdict `json:"verdict"`
	Title         string  `json:"title"`
	Detail        string  `json:"detail"`
	GatewayMs     float64 `json:"gateway_ms"`
	InternetMs    float64 `json:"internet_ms"`
	SignalPercent float64 `json:"signal_percent"` // 0 when unknown (cable)
}

// Above these, the path inside the home is the problem: a healthy router
// answers in a few milliseconds and loses nothing.
const (
	localLatencyLimit = 20.0 // ms
	localLossLimit    = 2.0  // percent
	// Internet latency this many times the good hours' (and this many ms
	// above it) means the path beyond the router degraded.
	providerFactor    = 2.0
	providerMinExcess = 30.0 // ms
	providerLossLimit = 2.0  // percent
)

func diagnose(buckets *[hoursInDay]bucket, hours *[hoursInDay]Hour, data *domain.DayData) Diagnosis {
	bad, good := splitHours(buckets, hours)
	var d Diagnosis
	if bad.rounds == 0 {
		all := mergeHours(buckets, 0, hoursInDay)
		d = Diagnosis{
			Verdict: VerdictNone, Title: "Nenhum problema encontrado",
			Detail:    "Nas horas monitoradas, sua internet atendeu ao que o seu perfil precisa.",
			GatewayMs: median(all.gateway), InternetMs: median(all.latency), SignalPercent: median(all.signal),
		}
		return d
	}

	d.GatewayMs, d.InternetMs, d.SignalPercent = median(bad.gateway), median(bad.latency), median(bad.signal)
	isWiFi := len(data.WiFi) > 0
	switch {
	case bad.gatewaySent == 0:
		d.Verdict, d.Title = VerdictUnknown, "Não deu para separar sua rede da operadora"
		d.Detail = "O roteador não foi medido nas piores horas."
	case d.GatewayMs > localLatencyLimit || bad.gatewayLossPercent() > localLossLimit:
		d.Verdict = VerdictLocal
		d.Title, d.Detail = localDiagnosis(&d, bad.gatewayLossPercent(), isWiFi)
	case providerDegraded(&bad, &good):
		d.Verdict, d.Title = VerdictProvider, "A lentidão veio da operadora"
		d.Detail = "Sua rede de casa esteve bem (o roteador respondeu em " + ptbr.Decimal(d.GatewayMs, 0) +
			" ms). A demora apareceu depois do roteador, no trecho da operadora. Trocar de roteador " +
			"ou mudar o computador de lugar não resolveria."
	default:
		d.Verdict, d.Title = VerdictCapacity, "A conexão respondeu bem, mas ficou lenta"
		d.Detail = "O tempo de resposta esteve normal, mas a velocidade ficou abaixo do que o seu perfil " +
			"precisa. Pode ser o limite do plano ou outros aparelhos usando a internet."
	}
	return d
}

func localDiagnosis(d *Diagnosis, gatewayLoss float64, isWiFi bool) (title, detail string) {
	evidence := "O roteador demorou " + ptbr.Decimal(d.GatewayMs, 0) + " ms para responder"
	if gatewayLoss > localLossLimit {
		evidence = "O roteador deixou de responder a " + ptbr.Decimal(gatewayLoss, 0) + "% dos pacotes"
	}
	if !isWiFi {
		return "O problema parece estar na sua rede local",
			evidence + ", antes mesmo de chegar à internet. Vale conferir o cabo e reiniciar o roteador."
	}
	detail = evidence + ", antes mesmo de chegar à internet."
	if d.SignalPercent > 0 {
		detail += " O sinal do Wi-Fi ficou em " + ptbr.Decimal(d.SignalPercent, 0) + "%."
	}
	return "O problema parece estar no seu Wi-Fi", detail + " Aproximar o computador do roteador pode ajudar."
}

// providerDegraded checks the path beyond the router, comparing with the
// good hours when there are any.
func providerDegraded(bad, good *bucket) bool {
	if bad.lossPercent() > providerLossLimit || bad.downtime > 0 {
		return true
	}
	if len(good.latency) == 0 {
		return false
	}
	current, reference := median(bad.latency), median(good.latency)
	return current >= providerFactor*reference && current-reference >= providerMinExcess
}

// splitHours separates the worst monitored hours (poor ones, or fair ones if
// nothing was poor) from the good ones.
func splitHours(buckets *[hoursInDay]bucket, hours *[hoursInDay]Hour) (bad, good bucket) {
	worst := domain.QualityPoor
	if !hasGrade(hours, domain.QualityPoor) {
		worst = domain.QualityFair
	}
	for h := range hours {
		switch hours[h].Grade {
		case worst:
			bad.merge(&buckets[h])
		case domain.QualityGood:
			good.merge(&buckets[h])
		case domain.QualityFair, domain.QualityPoor, domain.QualityUnknown:
		}
	}
	return bad, good
}

func hasGrade(hours *[hoursInDay]Hour, grade domain.Quality) bool {
	for h := range hours {
		if hours[h].Grade == grade {
			return true
		}
	}
	return false
}

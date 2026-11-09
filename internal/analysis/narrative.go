package analysis

import (
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/ptbr"
)

// headline sums up the day in one short sentence, like a newspaper would.
func headline(d *Daily) string {
	var known, unwell []*Period
	for i := range d.Periods {
		p := &d.Periods[i]
		switch p.Grade {
		case domain.QualityGood:
			known = append(known, p)
		case domain.QualityFair, domain.QualityPoor:
			known = append(known, p)
			unwell = append(unwell, p)
		case domain.QualityUnknown:
		}
	}

	switch {
	case len(known) == 0:
		return "Sem leitura suficiente."
	case len(unwell) == 0:
		return "Dia firme."
	case len(unwell) == len(known) && d.Grade == domain.QualityPoor:
		return "Dia difícil."
	case len(unwell) == len(known):
		return "Dia instável."
	case len(unwell) == 1:
		return "Dia firme, " + strings.ToLower(unwell[0].Name) + " instável."
	default:
		return "Dia de altos e baixos."
	}
}

// lede tells what happened: when it went well, when it did not and why.
func lede(d *Daily, buckets *[hoursInDay]bucket, profile domain.ProfileID) string {
	if len(d.Worst) == 0 {
		return "Sua internet ficou estável em todas as horas monitoradas."
	}

	var parts []string
	if len(d.Best) > 0 && d.Best[0].To-d.Best[0].From >= 2 {
		parts = append(parts, capitalize(rangeLabel(d.Best[0]))+" sua internet ficou estável.")
	}

	worst := d.Worst[0]
	bad := mergeHours(buckets, worst.From, worst.To)
	var reference bucket
	if len(d.Best) > 0 {
		reference = mergeHours(buckets, d.Best[0].From, d.Best[0].To)
	}
	parts = append(parts, capitalize(rangeLabel(worst))+" "+cause(&bad, &reference, d)+".")

	// Only a poor stretch justifies saying that something would have failed.
	if d.Hours[worst.From].Grade == domain.QualityPoor {
		if consequence := consequenceFor(profile); consequence != "" {
			parts = append(parts, consequence)
		}
	}
	return strings.Join(parts, " ")
}

// cause explains a bad stretch, comparing it with a good one when possible.
func cause(bad, reference *bucket, d *Daily) string {
	const (
		halved  = 0.5
		dropped = 0.8
	)
	badDown, refDown := median(bad.download), median(reference.download)
	badUp, refUp := median(bad.upload), median(reference.upload)
	badLatency, refLatency := median(bad.latency), median(reference.latency)

	switch {
	case bad.downtime > 0:
		return "a internet ficou fora do ar por " + durationWords(bad.downtime)
	case badDown > 0 && refDown > 0 && badDown < refDown*halved:
		return "a velocidade caiu para menos da metade (" + ptbr.Mbps(badDown) + ")"
	case badDown > 0 && refDown > 0 && badDown < refDown*dropped:
		return "a velocidade caiu para " + ptbr.Mbps(badDown)
	case badUp > 0 && refUp > 0 && badUp < refUp*halved:
		return "o upload caiu para menos da metade (" + ptbr.Mbps(badUp) + ")"
	case refLatency > 0 && badLatency >= refLatency*providerFactor:
		return "o tempo de resposta subiu para " + ptbr.Decimal(badLatency, 0) + " ms"
	case bad.lossPercent() > 1:
		return "houve perda de pacotes (" + ptbr.Percent(bad.lossPercent()) + ")"
	case median(bad.jitter) > 30:
		return "a conexão oscilou bastante (variação de " + ptbr.Decimal(median(bad.jitter), 0) + " ms)"
	default:
		return "a qualidade ficou abaixo do ideal para o perfil " + d.ProfileName
	}
}

func consequenceFor(profile domain.ProfileID) string {
	switch profile {
	case domain.ProfileHomeOffice:
		return "Uma videochamada nesse horário provavelmente teria travado."
	case domain.ProfileGaming:
		return "Jogar online nesse horário provavelmente teria sido ruim."
	case domain.ProfileStreaming:
		return "Um vídeo em alta qualidade nesse horário provavelmente teria travado."
	case domain.ProfileBasic, domain.ProfileComplete:
		return ""
	}
	return ""
}

// rangeLabel writes a stretch of hours: "das 20h às 22h", "às 13h".
func rangeLabel(r HourRange) string {
	if r.To == r.From+1 {
		return "por volta das " + hourWord(r.From)
	}
	start, end := "das "+hourWord(r.From), "às "+hourWord(r.To)
	if r.From == 0 {
		start = "da meia-noite"
	}
	if r.To == hoursInDay {
		end = "à meia-noite"
	}
	return start + " " + end
}

func hourWord(h int) string { return strconv.Itoa(h) + "h" }

func durationWords(d time.Duration) string {
	minutes := int(d.Round(time.Minute) / time.Minute)
	switch {
	case minutes < 1:
		return "menos de 1 minuto"
	case minutes == 1:
		return "1 minuto"
	case minutes < 60:
		return strconv.Itoa(minutes) + " minutos"
	default:
		return ptbr.Hours(d)
	}
}

func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

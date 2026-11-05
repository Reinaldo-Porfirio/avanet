package tray

import (
	"github.com/Reinaldo-Porfirio/avanet/internal/agent"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/ptbr"
)

// maxTooltip is the Windows limit for a notification-area tooltip.
const maxTooltip = 127

func qualityWord(q domain.Quality) string {
	switch q {
	case domain.QualityGood:
		return "boa"
	case domain.QualityFair:
		return "regular"
	case domain.QualityPoor:
		return "ruim"
	case domain.QualityUnknown:
		return "sem dados"
	}
	return "sem dados"
}

// statusLine summarizes the latest round in plain Portuguese.
func statusLine(s *agent.Status) string {
	switch {
	case s.At.IsZero():
		return "Aguardando a primeira medição…"
	case s.Outage:
		return "Sem conexão com a internet"
	case s.BestTarget == "":
		return "Internet não respondeu na última medição"
	default:
		return "Internet: " + ptbr.Millis(s.Internet.Median) + " · " + qualityWord(s.Quality)
	}
}

// speedLine describes the latest speed test or why none ran.
func speedLine(s *agent.Status) string {
	switch {
	case s.SpeedNotice != "":
		return s.SpeedNotice
	case s.LastSpeed != nil:
		r := s.LastSpeed
		return "Velocidade: " + ptbr.Decimal(r.DownloadMbps, 0) + " ↓ / " + ptbr.Decimal(r.UploadMbps, 0) +
			" ↑ Mbps às " + r.Start.Local().Format("15:04")
	default:
		return "Nenhum teste de velocidade ainda"
	}
}

func tooltip(s *agent.Status) string {
	text := "AvaNet · " + statusLine(s)
	if r := []rune(text); len(r) > maxTooltip {
		text = string(r[:maxTooltip-1]) + "…"
	}
	return text
}

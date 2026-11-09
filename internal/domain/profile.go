package domain

import "time"

// ProfileID identifies a usage profile; it is stored in the settings table.
type ProfileID string

// Usage profiles (plan, section 5).
const (
	ProfileBasic      ProfileID = "basico"
	ProfileStreaming  ProfileID = "streaming"
	ProfileGaming     ProfileID = "jogos"
	ProfileHomeOffice ProfileID = "homeoffice"
	ProfileComplete   ProfileID = "completo"

	DefaultProfile = ProfileBasic
)

// Profile sets how often AvaNet measures. The texts are shown to users, so
// they are in Portuguese and avoid jargon.
type Profile struct {
	ID          ProfileID
	Name        string
	Description string
	// LightInterval is the pause between latency rounds (a few KB each).
	LightInterval time.Duration
	// SpeedInterval is the desired pause between speed tests. The scheduler
	// stretches it when the daily data budget would not last the day.
	SpeedInterval time.Duration
	// Limits say what counts as good or poor in the reports.
	Limits Limits
}

// Profiles returns every profile, in the order they are offered to users.
func Profiles() []Profile {
	profiles := []Profile{
		{
			ID: ProfileBasic, Name: "Uso básico",
			Description:   "Navegar, redes sociais, WhatsApp e e-mail. Mede pouco e economiza recursos.",
			LightInterval: 5 * time.Minute, SpeedInterval: 3 * time.Hour,
		},
		{
			ID: ProfileStreaming, Name: "Streaming",
			Description:   "Assistir Netflix, YouTube e afins. Foca em velocidade de download estável para o vídeo não travar.",
			LightInterval: 5 * time.Minute, SpeedInterval: 90 * time.Minute,
		},
		{
			ID: ProfileGaming, Name: "Jogos online",
			Description:   "Para quem joga online. Foca no tempo de resposta (ping) e em travadinhas (jitter e perda), que importam mais que velocidade.",
			LightInterval: time.Minute, SpeedInterval: 3 * time.Hour,
		},
		{
			ID: ProfileHomeOffice, Name: "Home office",
			Description:   "Videochamadas e trabalho remoto. Foca em upload e estabilidade, que é o que faz a chamada travar ou cortar.",
			LightInterval: 2 * time.Minute, SpeedInterval: 90 * time.Minute,
		},
		{
			ID: ProfileComplete, Name: "Completo",
			Description:   "Mede tudo, o tempo todo, dentro do limite de dados. Para quem quer todos os números.",
			LightInterval: time.Minute, SpeedInterval: time.Hour,
		},
	}
	for i := range profiles {
		profiles[i].Limits = limitsFor(profiles[i].ID)
	}
	return profiles
}

// ProfileByID finds a profile.
func ProfileByID(id ProfileID) (Profile, bool) {
	profiles := Profiles()
	for i := range profiles {
		if profiles[i].ID == id {
			return profiles[i], true
		}
	}
	return Profile{}, false
}

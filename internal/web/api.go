package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/agent"
	"github.com/Reinaldo-Porfirio/avanet/internal/analysis"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/ptbr"
	"github.com/Reinaldo-Porfirio/avanet/internal/storage"
)

const (
	maxBodyBytes = 16 << 10
	// deleteConfirmation must be sent to erase the history: a second lock
	// besides the dialog on the page.
	deleteConfirmation = "APAGAR"
)

// storageTrigger keeps the Store interface readable.
type storageTrigger = storage.SpeedTrigger

type (
	speedView struct {
		At           time.Time `json:"at"`
		DownloadMbps float64   `json:"download_mbps"`
		UploadMbps   float64   `json:"upload_mbps"`
		LocalTraffic bool      `json:"local_traffic"`
	}
	usageView struct {
		Mobile     bool  `json:"mobile"`
		Allowed    bool  `json:"allowed"`
		UsedBytes  int64 `json:"used_bytes"`
		LimitBytes int64 `json:"limit_bytes"`
	}
	profileRef struct {
		ID   domain.ProfileID `json:"id"`
		Name string           `json:"name"`
	}
	nowResponse struct {
		Measured      bool           `json:"measured"`
		At            *time.Time     `json:"at,omitempty"`
		Quality       domain.Quality `json:"quality"`
		Outage        bool           `json:"outage"`
		LatencyMs     float64        `json:"latency_ms"`
		JitterMs      float64        `json:"jitter_ms"`
		LossPercent   float64        `json:"loss_percent"`
		Target        string         `json:"target,omitempty"`
		GatewayMs     *float64       `json:"gateway_ms,omitempty"`
		Connection    string         `json:"connection,omitempty"`
		SignalPercent int            `json:"signal_percent,omitempty"`

		LastSpeed    *speedView `json:"last_speed,omitempty"`
		SpeedRunning bool       `json:"speed_running"`
		SpeedNotice  string     `json:"speed_notice,omitempty"`

		Profile          profileRef `json:"profile"`
		ProfileChosen    bool       `json:"profile_chosen"`
		Today            domain.Day `json:"today"`
		Data             usageView  `json:"data"`
		MonitoredTodayMs float64    `json:"monitored_today_ms"`
		ElapsedTodayMs   float64    `json:"elapsed_today_ms"`
	}
	profileView struct {
		ID          domain.ProfileID `json:"id"`
		Name        string           `json:"name"`
		Description string           `json:"description"`
		Light       string           `json:"light_interval"`
		Speed       string           `json:"speed_interval"`
	}
	limitsView struct {
		MinFixedDailyMB  int64   `json:"min_fixed_daily_mb"`
		MaxFixedDailyMB  int64   `json:"max_fixed_daily_mb"`
		MinMobileDailyMB int64   `json:"min_mobile_daily_mb"`
		MaxMobileDailyMB int64   `json:"max_mobile_daily_mb"`
		MaxPlanMbps      float64 `json:"max_plan_mbps"`
	}
	autostartView struct {
		Available bool `json:"available"`
		Enabled   bool `json:"enabled"`
	}
	settingsResponse struct {
		Preferences domain.Preferences `json:"preferences"`
		Profiles    []profileView      `json:"profiles"`
		Limits      limitsView         `json:"limits"`
		Autostart   autostartView      `json:"autostart"`
	}
)

func millis(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func (s *Server) handleNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := s.deps.Now()
	status := s.deps.Agent.Status()

	prefs, err := s.preferences(r)
	if err != nil {
		s.fail(w, "read preferences", err)
		return
	}
	profile, _ := domain.ProfileByID(prefs.Profile) // Preferences only returns known profiles
	_, chosen, err := s.deps.Store.Setting(ctx, storage.SettingProfile)
	if err != nil {
		s.fail(w, "read profile", err)
		return
	}
	midnight := domain.StartOfLocalDay(now)
	coverage, err := s.deps.Store.Coverage(ctx, midnight, now)
	if err != nil {
		s.fail(w, "read coverage", err)
		return
	}
	usage, err := s.usage(r, &status, prefs, now)
	if err != nil {
		s.fail(w, "read data usage", err)
		return
	}
	lastSpeed, err := s.lastSpeed(r, &status)
	if err != nil {
		s.fail(w, "read last speed test", err)
		return
	}

	response := nowResponse{
		Measured: !status.At.IsZero(), Quality: status.Quality, Outage: status.Outage,
		Connection: connectionLabel(&status), SignalPercent: status.SignalPercent,
		LastSpeed: lastSpeed, SpeedRunning: status.SpeedRunning, SpeedNotice: status.SpeedNotice,
		Profile: profileRef{ID: profile.ID, Name: profile.Name}, ProfileChosen: chosen,
		Today: domain.DayOf(now), Data: usage,
		MonitoredTodayMs: millis(coverage), ElapsedTodayMs: millis(now.Sub(midnight)),
	}
	if response.Measured {
		response.At = &status.At
	}
	if status.BestTarget != "" {
		response.Target = status.BestTarget
		response.LatencyMs, response.JitterMs = millis(status.Internet.Median), millis(status.Internet.Jitter)
		response.LossPercent = status.Internet.LossPercent()
	}
	if status.HasGateway && status.Gateway.Reachable() {
		gateway := millis(status.Gateway.Median)
		response.GatewayMs = &gateway
	}
	s.writeJSON(w, http.StatusOK, &response)
}

// preferences logs values that had to be replaced by their defaults.
func (s *Server) preferences(r *http.Request) (domain.Preferences, error) {
	prefs, invalid, err := s.deps.Store.Preferences(r.Context())
	if len(invalid) > 0 {
		s.deps.Logger.Warn("invalid stored settings replaced by defaults", "keys", invalid)
	}
	return prefs, err
}

func (s *Server) usage(r *http.Request, status *agent.Status, prefs domain.Preferences, now time.Time) (usageView, error) {
	class := domain.ClassOf(status.Network)
	used, err := s.deps.Store.DataUsage(r.Context(), domain.DayOf(now), class)
	if err != nil {
		return usageView{}, err
	}
	budget := prefs.Budget()
	return usageView{
		Mobile: class == domain.DataMobile, Allowed: class == domain.DataFixed || budget.MobileEnabled,
		UsedBytes: used, LimitBytes: budget.Daily(class),
	}, nil
}

// lastSpeed prefers the agent's value and falls back to the database, because
// the agent only knows the tests run since it started.
func (s *Server) lastSpeed(r *http.Request, status *agent.Status) (*speedView, error) {
	result := status.LastSpeed
	if result == nil {
		stored, _, found, err := s.deps.Store.LatestSpeedTest(r.Context())
		if err != nil || !found {
			return nil, err
		}
		result = &stored
	}
	return &speedView{At: result.Start, DownloadMbps: result.DownloadMbps, UploadMbps: result.UploadMbps, LocalTraffic: result.LocalTraffic}, nil
}

func connectionLabel(status *agent.Status) string {
	if status.At.IsZero() {
		return ""
	}
	switch status.Network.Kind {
	case domain.ConnectionWiFi:
		if status.SSID != "" {
			return "Wi-Fi “" + status.SSID + "”"
		}
		return "Wi-Fi"
	case domain.ConnectionEthernet:
		return "Cabo"
	case domain.ConnectionMobile:
		return "Modem móvel"
	case domain.ConnectionOther:
		return "Outra conexão"
	}
	return ""
}

func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	now := s.deps.Now()
	days, err := s.deps.Reports.Days(r.Context(), now)
	if err != nil {
		s.fail(w, "list reports", err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"today": domain.DayOf(now), "days": days})
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	day := domain.Day(r.PathValue("day"))
	if !day.Valid() {
		s.writeError(w, http.StatusBadRequest, "Data inválida.")
		return
	}
	daily, err := s.deps.Reports.Load(r.Context(), day, s.deps.Now())
	if errors.Is(err, analysis.ErrNoData) {
		s.writeError(w, http.StatusNotFound, "Não há medições neste dia.")
		return
	}
	if err != nil {
		s.fail(w, "load report", err)
		return
	}
	s.writeJSON(w, http.StatusOK, daily)
}

// handleInsights answers with the patterns and the plan comparison up to the
// day in ?until= (yesterday when absent).
func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) {
	now := s.deps.Now()
	until := domain.DayOf(now)
	if requested := r.URL.Query().Get("until"); requested != "" {
		until = domain.Day(requested)
	}
	if !until.Valid() {
		s.writeError(w, http.StatusBadRequest, "Data inválida.")
		return
	}
	prefs, err := s.preferences(r)
	if err != nil {
		s.fail(w, "read preferences", err)
		return
	}
	insights, err := s.deps.Reports.Insights(r.Context(), until, now, prefs.PlanDownloadMbps, prefs.PlanUploadMbps)
	if err != nil {
		s.fail(w, "build insights", err)
		return
	}
	s.writeJSON(w, http.StatusOK, insights)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.preferences(r)
	if err != nil {
		s.fail(w, "read preferences", err)
		return
	}
	s.writeJSON(w, http.StatusOK, &settingsResponse{
		Preferences: prefs,
		Profiles:    profileViews(),
		Limits: limitsView{
			MinFixedDailyMB: domain.MinFixedDailyMB, MaxFixedDailyMB: domain.MaxFixedDailyMB,
			MinMobileDailyMB: domain.MinMobileDailyMB, MaxMobileDailyMB: domain.MaxMobileDailyMB,
			MaxPlanMbps: domain.MaxPlanMbps,
		},
		Autostart: s.autostartView(),
	})
}

func profileViews() []profileView {
	profiles := domain.Profiles()
	views := make([]profileView, 0, len(profiles))
	for i := range profiles {
		p := &profiles[i]
		views = append(views, profileView{
			ID: p.ID, Name: p.Name, Description: p.Description,
			Light: ptbr.Interval(p.LightInterval), Speed: ptbr.Interval(p.SpeedInterval),
		})
	}
	return views
}

func (s *Server) autostartView() autostartView {
	enabled, err := s.deps.Autostart.Enabled()
	if err != nil {
		s.deps.Logger.Debug("autostart unavailable", "error", err)
		return autostartView{}
	}
	return autostartView{Available: true, Enabled: enabled}
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var prefs domain.Preferences
	if !s.readJSON(w, r, &prefs) {
		return
	}
	if err := prefs.Validate(); err != nil {
		s.writeError(w, http.StatusBadRequest, "Algum valor está fora do permitido. Confira os campos.")
		return
	}
	profile, _ := domain.ProfileByID(prefs.Profile) // checked by Validate
	if err := s.deps.Store.SavePreferences(r.Context(), prefs, s.deps.Now()); err != nil {
		s.fail(w, "save preferences", err)
		return
	}
	if err := s.deps.Agent.ApplySettings(&profile, prefs.Budget()); err != nil {
		s.fail(w, "apply settings", err)
		return
	}
	s.deps.Logger.Info("preferences changed", "profile", prefs.Profile, "mobile_enabled", prefs.MobileEnabled)
	s.writeJSON(w, http.StatusOK, &prefs)
}

func (s *Server) handleAutostart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !s.readJSON(w, r, &body) {
		return
	}
	err := s.deps.Autostart.SetEnabled(body.Enabled)
	if errors.Is(err, ErrAutostartUnavailable) {
		s.writeError(w, http.StatusConflict, "Esta cópia do programa não pode iniciar com o Windows. Use a versão instalada.")
		return
	}
	if err != nil {
		s.fail(w, "change autostart", err)
		return
	}
	s.writeJSON(w, http.StatusOK, s.autostartView())
}

func (s *Server) handleSpeedTest(w http.ResponseWriter, _ *http.Request) {
	s.deps.Agent.RequestSpeedTest()
	s.writeJSON(w, http.StatusAccepted, map[string]bool{"requested": true})
}

func (s *Server) handleDeleteHistory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Confirm string `json:"confirm"`
	}
	if !s.readJSON(w, r, &body) {
		return
	}
	if body.Confirm != deleteConfirmation {
		s.writeError(w, http.StatusBadRequest, "Confirmação incorreta. Nada foi apagado.")
		return
	}
	if err := s.deps.Store.DeleteHistory(r.Context(), domain.DayOf(s.deps.Now())); err != nil {
		s.fail(w, "delete history", err)
		return
	}
	s.deps.Logger.Info("measurement history deleted by the user")
	// The history is gone either way; a failure here only delays reclaiming
	// the file's free space, so the user is not told it failed.
	if err := s.deps.Store.Compact(r.Context()); err != nil {
		s.deps.Logger.Warn("compact after deleting history failed", "error", err)
	}
	s.writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// readJSON decodes a request body strictly: size-limited, one JSON value and
// no unknown fields. On failure it has already answered the request.
func (s *Server) readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(target)
	if err == nil && !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		err = errors.New("more than one JSON value")
	}
	if err != nil {
		s.deps.Logger.Debug("rejected request body", "path", r.URL.Path, "error", err)
		s.writeError(w, http.StatusBadRequest, "Pedido inválido.")
		return false
	}
	return true
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	// Encoded first, so a failure can still become a proper error response.
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(value); err != nil {
		s.fail(w, "encode response", err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := body.WriteTo(w); err != nil {
		s.deps.Logger.Debug("send response failed", "error", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"error": message})
}

// fail logs the cause and tells the page only that something went wrong.
func (s *Server) fail(w http.ResponseWriter, operation string, err error) {
	s.deps.Logger.Error(fmt.Sprintf("panel: %s failed", operation), "error", err)
	s.writeError(w, http.StatusInternalServerError, "Algo deu errado. Tente de novo em instantes.")
}

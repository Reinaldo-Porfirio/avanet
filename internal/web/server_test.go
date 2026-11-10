package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/agent"
	"github.com/Reinaldo-Porfirio/avanet/internal/analysis"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/report"
	"github.com/Reinaldo-Porfirio/avanet/internal/storage"
)

type fakeAgent struct {
	mu       sync.Mutex
	status   agent.Status
	requests int
	profiles []domain.ProfileID
	budgets  []domain.DataBudget
}

func (a *fakeAgent) Status() agent.Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func (a *fakeAgent) RequestSpeedTest() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests++
}

func (a *fakeAgent) ApplySettings(p *domain.Profile, b domain.DataBudget) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.profiles, a.budgets = append(a.profiles, p.ID), append(a.budgets, b)
	return nil
}

type fakeReports struct {
	daily *analysis.Daily
	err   error
	// What Insights was asked for.
	until            domain.Day
	planDown, planUp float64
}

func (r *fakeReports) Insights(_ context.Context, until domain.Day, _ time.Time, down, up float64) (*analysis.Insights, error) {
	r.until, r.planDown, r.planUp = until, down, up
	return &analysis.Insights{Until: until, Patterns: analysis.PatternReport{Patterns: []analysis.Pattern{}}}, r.err
}

func (r *fakeReports) Load(context.Context, domain.Day, time.Time) (*analysis.Daily, error) {
	return r.daily, r.err
}

func (r *fakeReports) Days(context.Context, time.Time) ([]report.DayEntry, error) {
	return []report.DayEntry{{Day: "2026-11-08", Grade: "good"}}, r.err
}

type fakeAutostart struct {
	on  bool
	err error
}

func (a *fakeAutostart) Enabled() (bool, error) { return a.on, nil }
func (a *fakeAutostart) SetEnabled(on bool) error {
	if a.err != nil {
		return a.err
	}
	a.on = on
	return nil
}

type harness struct {
	t         *testing.T
	server    *Server
	client    *http.Client
	agent     *fakeAgent
	reports   *fakeReports
	autostart *fakeAutostart
	db        *storage.DB
	now       time.Time
	nowMu     sync.Mutex
}

func (h *harness) clock() time.Time {
	h.nowMu.Lock()
	defer h.nowMu.Unlock()
	return h.now
}

func (h *harness) advance(d time.Duration) {
	h.nowMu.Lock()
	defer h.nowMu.Unlock()
	h.now = h.now.Add(d)
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "avanet.db"))
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		t: t, db: db, agent: &fakeAgent{}, reports: &fakeReports{}, autostart: &fakeAutostart{},
		now:    time.Date(2026, 11, 9, 15, 0, 0, 0, time.Local),
		client: &http.Client{Jar: jar, Timeout: 5 * time.Second},
	}

	server, err := New(&Deps{
		Agent: h.agent, Reports: h.reports, Store: db, Autostart: h.autostart,
		Assets: fstest.MapFS{
			"index.html": {Data: []byte("<h1>painel</h1>")},
			"app.js":     {Data: []byte("1")},
			"styles.css": {Data: []byte("a{}")},
			"secret.txt": {Data: []byte("not servable")},
			"font.woff2": {Data: []byte("x")},
			"sub/app.js": {Data: []byte("2")},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    h.clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Listen(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.server = server

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return h
}

func (h *harness) url(path string) string { return "http://" + h.server.host + path }

type result struct {
	status int
	body   string
	header http.Header
}

func (h *harness) send(req *http.Request) result {
	h.t.Helper()
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatal(err)
	}
	return result{resp.StatusCode, string(body), resp.Header}
}

func (h *harness) request(method, path, body string) *http.Request {
	h.t.Helper()
	req, err := http.NewRequestWithContext(h.t.Context(), method, h.url(path), strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	return req
}

// call makes a request the way the panel's own page does.
func (h *harness) call(method, path, body string) result {
	h.t.Helper()
	req := h.request(method, path, body)
	req.Header.Set("Origin", "http://"+h.server.host)
	return h.send(req)
}

func (h *harness) login() {
	h.t.Helper()
	req, err := http.NewRequestWithContext(h.t.Context(), http.MethodGet, h.server.OpenURL(time.Minute), http.NoBody)
	if err != nil {
		h.t.Fatal(err)
	}
	if got := h.send(req); got.status != http.StatusOK {
		h.t.Fatalf("login: %d %s", got.status, got.body)
	}
}

func TestListensOnlyOnLoopback(t *testing.T) {
	h := newHarness(t)
	if !strings.HasPrefix(h.server.host, "127.0.0.1:") {
		t.Errorf("listening on %s", h.server.host)
	}
}

func TestEverythingNeedsASession(t *testing.T) {
	h := newHarness(t)
	for _, target := range [][2]string{
		{http.MethodGet, "/"}, {http.MethodGet, "/app.js"}, {http.MethodGet, "/api/now"},
		{http.MethodGet, "/api/settings"}, {http.MethodPost, "/api/speedtest"},
	} {
		if got := h.call(target[0], target[1], ""); got.status != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", target[0], target[1], got.status)
		}
	}
	if h.agent.requests != 0 {
		t.Error("an unauthenticated request reached the agent")
	}
}

func TestForgedSessionIsRefused(t *testing.T) {
	h := newHarness(t)
	req := h.request(http.MethodGet, "/api/now", "")
	req.AddCookie(&http.Cookie{Name: h.server.cookieName, Value: "guess"})
	if got := h.send(req); got.status != http.StatusUnauthorized {
		t.Errorf("status = %d", got.status)
	}
}

func TestTicketOpensThePanelOnce(t *testing.T) {
	h := newHarness(t)
	link := strings.TrimPrefix(h.server.OpenURL(time.Minute), "http://"+h.server.host)

	resp := h.call(http.MethodGet, link, "")
	if resp.status != http.StatusOK || !strings.Contains(resp.body, "painel") {
		t.Fatalf("first use: %d %q", resp.status, resp.body)
	}
	if got := h.call(http.MethodGet, "/api/now", ""); got.status != http.StatusOK {
		t.Errorf("session after the ticket: %d", got.status)
	}

	other := &http.Client{Timeout: 5 * time.Second} // another browser, no cookie
	h.client = other
	if got := h.call(http.MethodGet, link, ""); got.status != http.StatusUnauthorized {
		t.Errorf("second use: %d, want 401", got.status)
	}
}

func TestTicketExpiresAndGuessesFail(t *testing.T) {
	h := newHarness(t)
	link := strings.TrimPrefix(h.server.OpenURL(time.Minute), "http://"+h.server.host)
	h.advance(time.Minute + time.Second)
	if got := h.call(http.MethodGet, link, ""); got.status != http.StatusUnauthorized {
		t.Errorf("expired ticket: %d", got.status)
	}
	for _, guess := range []string{"/open", "/open?ticket=", "/open?ticket=AAAA"} {
		if got := h.call(http.MethodGet, guess, ""); got.status != http.StatusUnauthorized {
			t.Errorf("%s = %d", guess, got.status)
		}
	}
}

func TestOldTicketsAreDiscarded(t *testing.T) {
	h := newHarness(t)
	first := strings.TrimPrefix(h.server.OpenURL(time.Minute), "http://"+h.server.host)
	for range maxTickets {
		h.server.OpenURL(time.Minute)
	}
	if got := h.call(http.MethodGet, first, ""); got.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want the oldest ticket gone", got.status)
	}
}

func TestSessionCookieIsProtected(t *testing.T) {
	h := newHarness(t)
	h.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, h.server.OpenURL(time.Minute), http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	got := h.send(req)
	if got.status != http.StatusSeeOther || got.header.Get("Location") != "/" {
		t.Fatalf("redirect: %d to %q (the ticket must leave the address bar)", got.status, got.header.Get("Location"))
	}

	cookies := (&http.Response{Header: got.header}).Cookies()
	if len(cookies) != 1 {
		t.Fatalf("%d cookies", len(cookies))
	}
	c := cookies[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge != 0 || !c.Expires.IsZero() {
		t.Errorf("cookie = %+v", c)
	}
	if !strings.HasSuffix(c.Name, h.server.host[strings.LastIndex(h.server.host, ":")+1:]) {
		t.Errorf("cookie name %q must carry the port", c.Name)
	}
}

func TestDNSRebindingIsBlocked(t *testing.T) {
	h := newHarness(t)
	h.login()
	for _, host := range []string{"evil.example", "localhost", "127.0.0.1", "127.0.0.1:1"} {
		req := h.request(http.MethodGet, "/api/now", "")
		req.Host = host
		if got := h.send(req); got.status != http.StatusForbidden {
			t.Errorf("Host %q = %d, want 403", host, got.status)
		}
	}
}

func TestChangesNeedTheSameOrigin(t *testing.T) {
	h := newHarness(t)
	h.login()
	for _, origin := range []string{"", "null", "http://evil.example", "http://127.0.0.1", "https://" + h.server.host} {
		req := h.request(http.MethodPost, "/api/speedtest", "")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if got := h.send(req); got.status != http.StatusForbidden {
			t.Errorf("Origin %q = %d, want 403", origin, got.status)
		}
	}
	if h.agent.requests != 0 {
		t.Error("a cross-site request reached the agent")
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	h := newHarness(t)
	check := func(name string, got result) {
		t.Helper()
		want := map[string]string{
			"X-Content-Type-Options":       "nosniff",
			"X-Frame-Options":              "DENY",
			"Referrer-Policy":              "no-referrer",
			"Cross-Origin-Resource-Policy": "same-origin",
			"Cache-Control":                "no-store",
		}
		for header, value := range want {
			if got.header.Get(header) != value {
				t.Errorf("%s: %s = %q", name, header, got.header.Get(header))
			}
		}
		csp := got.header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "unsafe") {
			t.Errorf("%s: CSP = %q", name, csp)
		}
		if got.header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: CORS must stay off", name)
		}
	}
	check("refused", h.call(http.MethodGet, "/api/now", ""))
	h.login()
	check("api", h.call(http.MethodGet, "/api/now", ""))
	check("page", h.call(http.MethodGet, "/", ""))
	check("missing", h.call(http.MethodGet, "/nope.js", ""))
}

func TestAssets(t *testing.T) {
	h := newHarness(t)
	h.login()
	for path, want := range map[string]string{
		"/":           "text/html; charset=utf-8",
		"/app.js":     "text/javascript; charset=utf-8",
		"/styles.css": "text/css; charset=utf-8",
		"/font.woff2": "font/woff2",
		"/sub/app.js": "text/javascript; charset=utf-8",
	} {
		got := h.call(http.MethodGet, path, "")
		if got.status != http.StatusOK || got.header.Get("Content-Type") != want {
			t.Errorf("%s: %d %q", path, got.status, got.header.Get("Content-Type"))
		}
	}
	for _, path := range []string{"/secret.txt", "/missing.js", "/..%2f..%2fapp.js", "/../../etc/index.html"} {
		req := h.request(http.MethodGet, "/", "")
		req.URL.Opaque = path // sent exactly as written, without being cleaned
		if got := h.send(req); got.status != http.StatusNotFound {
			t.Errorf("%s = %d, want not served", path, got.status)
		}
	}
}

func TestNowBeforeTheFirstMeasurement(t *testing.T) {
	h := newHarness(t)
	h.login()
	got := h.call(http.MethodGet, "/api/now", "")
	if got.status != http.StatusOK || !strings.Contains(got.body, `"measured":false`) || !strings.Contains(got.body, `"profile_chosen":false`) {
		t.Errorf("%d %s", got.status, got.body)
	}
}

func TestNowDescribesTheConnection(t *testing.T) {
	h := newHarness(t)
	h.login()
	stats := domain.LatencyStats{Sent: 4, Received: 4, Median: 12 * time.Millisecond, Jitter: 2 * time.Millisecond}
	h.agent.status = agent.Status{
		At: h.now, BestTarget: "Cloudflare", Internet: stats, Quality: domain.QualityGood,
		HasGateway: true, Gateway: domain.LatencyStats{Sent: 4, Received: 4, Median: 3 * time.Millisecond},
		Network: domain.Network{Kind: domain.ConnectionWiFi, InterfaceName: "Wi-Fi"}, SSID: "Casa", SignalPercent: 80,
	}
	got := h.call(http.MethodGet, "/api/now", "")
	for _, want := range []string{`"latency_ms":12`, `"gateway_ms":3`, `"quality":"good"`, `"signal_percent":80`, `Casa`, `"target":"Cloudflare"`} {
		if !strings.Contains(got.body, want) {
			t.Errorf("missing %s in %s", want, got.body)
		}
	}
}

const validSettings = `{"profile":"jogos","fixed_daily_mb":1000,"mobile_enabled":true,"mobile_daily_mb":100,"plan_download_mbps":500,"plan_upload_mbps":50}`

func TestSaveSettingsStoresAndAppliesImmediately(t *testing.T) {
	h := newHarness(t)
	h.login()
	if got := h.call(http.MethodPut, "/api/settings", validSettings); got.status != http.StatusOK {
		t.Fatalf("%d %s", got.status, got.body)
	}
	if len(h.agent.profiles) != 1 || h.agent.profiles[0] != domain.ProfileGaming || !h.agent.budgets[0].MobileEnabled {
		t.Errorf("agent got %v %+v", h.agent.profiles, h.agent.budgets)
	}
	prefs, _, err := h.db.Preferences(t.Context())
	if err != nil || prefs.Profile != domain.ProfileGaming || prefs.FixedDailyMB != 1000 || prefs.PlanDownloadMbps != 500 {
		t.Errorf("stored %+v, %v", prefs, err)
	}
	if got := h.call(http.MethodGet, "/api/now", ""); !strings.Contains(got.body, `"profile_chosen":true`) {
		t.Errorf("after choosing: %s", got.body)
	}
}

func TestSaveSettingsRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	h.login()
	huge := `{"profile":"` + strings.Repeat("a", maxBodyBytes) + `"}`
	for name, body := range map[string]string{
		"not json":        "nope",
		"empty":           "",
		"two values":      validSettings + validSettings,
		"unknown field":   strings.Replace(validSettings, `"profile"`, `"admin":true,"profile"`, 1),
		"unknown profile": strings.Replace(validSettings, "jogos", "x", 1),
		"budget too big":  strings.Replace(validSettings, `"fixed_daily_mb":1000`, `"fixed_daily_mb":999999`, 1),
		"mobile too big":  strings.Replace(validSettings, `"mobile_daily_mb":100`, `"mobile_daily_mb":201`, 1),
		"negative plan":   strings.Replace(validSettings, `"plan_upload_mbps":50`, `"plan_upload_mbps":-1`, 1),
		"too large":       huge,
	} {
		if got := h.call(http.MethodPut, "/api/settings", body); got.status != http.StatusBadRequest {
			t.Errorf("%s = %d", name, got.status)
		}
	}
	if len(h.agent.profiles) != 0 {
		t.Error("invalid settings reached the agent")
	}
	if _, chosen, err := h.db.Setting(t.Context(), storage.SettingProfile); err != nil || chosen {
		t.Errorf("invalid settings were stored: chosen=%v err=%v", chosen, err)
	}
}

func TestSettingsResponse(t *testing.T) {
	h := newHarness(t)
	h.login()
	got := h.call(http.MethodGet, "/api/settings", "")
	for _, want := range []string{`"id":"homeoffice"`, `"light_interval":"5 minutos"`, `"max_mobile_daily_mb":200`, `"available":true`} {
		if !strings.Contains(got.body, want) {
			t.Errorf("missing %s in %s", want, got.body)
		}
	}
}

func TestReports(t *testing.T) {
	h := newHarness(t)
	h.login()
	if got := h.call(http.MethodGet, "/api/reports", ""); got.status != http.StatusOK || !strings.Contains(got.body, "2026-11-08") {
		t.Errorf("list: %d %s", got.status, got.body)
	}

	h.reports.daily = &analysis.Daily{Day: "2026-11-08", Headline: "Dia firme."}
	if got := h.call(http.MethodGet, "/api/reports/2026-11-08", ""); got.status != http.StatusOK || !strings.Contains(got.body, "Dia firme") {
		t.Errorf("report: %d %s", got.status, got.body)
	}
	if got := h.call(http.MethodGet, "/api/reports/not-a-day", ""); got.status != http.StatusBadRequest {
		t.Errorf("bad day: %d", got.status)
	}

	h.reports.err = analysis.ErrNoData
	if got := h.call(http.MethodGet, "/api/reports/2026-11-01", ""); got.status != http.StatusNotFound {
		t.Errorf("no data: %d", got.status)
	}
}

func TestInsightsUseThePlanAndValidateTheDay(t *testing.T) {
	h := newHarness(t)
	h.login()
	if got := h.call(http.MethodPut, "/api/settings", validSettings); got.status != http.StatusOK {
		t.Fatal(got.body)
	}
	if got := h.call(http.MethodGet, "/api/insights?until=2026-11-07", ""); got.status != http.StatusOK || !strings.Contains(got.body, `"until":"2026-11-07"`) {
		t.Errorf("%d %s", got.status, got.body)
	}
	if h.reports.planDown != 500 || h.reports.planUp != 50 || h.reports.until != "2026-11-07" {
		t.Errorf("asked for %+v", h.reports)
	}
	if got := h.call(http.MethodGet, "/api/insights", ""); got.status != http.StatusOK || h.reports.until != domain.DayOf(h.now) {
		t.Errorf("default day: %d, until %s", got.status, h.reports.until)
	}
	if got := h.call(http.MethodGet, "/api/insights?until=amanha", ""); got.status != http.StatusBadRequest {
		t.Errorf("bad day: %d", got.status)
	}
}

func TestInternalErrorsStayInTheLog(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.reports.err = errors.New(`open C:\Users\someone\secret.db: boom`)
	got := h.call(http.MethodGet, "/api/reports/2026-11-01", "")
	if got.status != http.StatusInternalServerError || strings.Contains(got.body, "secret") {
		t.Errorf("%d %s", got.status, got.body)
	}
}

func TestSpeedTestRequest(t *testing.T) {
	h := newHarness(t)
	h.login()
	if got := h.call(http.MethodPost, "/api/speedtest", ""); got.status != http.StatusAccepted {
		t.Errorf("status = %d", got.status)
	}
	if h.agent.requests != 1 {
		t.Errorf("%d requests", h.agent.requests)
	}
}

func TestDeleteHistoryNeedsConfirmation(t *testing.T) {
	h := newHarness(t)
	h.login()
	ctx := t.Context()
	sample := []domain.TargetLatency{{Target: "Cloudflare", Stats: domain.LatencyStats{Sent: 1, Received: 1, Median: time.Millisecond}}}
	if err := h.db.SaveLatency(ctx, h.now, 0, sample); err != nil {
		t.Fatal(err)
	}
	day := domain.StartOfLocalDay(h.now)
	measured := func() bool {
		has, err := h.db.HasMeasurements(ctx, day, day.AddDate(0, 0, 1))
		if err != nil {
			t.Fatal(err)
		}
		return has
	}

	for _, body := range []string{`{"confirm":"apagar"}`, `{"confirm":""}`, `{}`} {
		if got := h.call(http.MethodPost, "/api/history/delete", body); got.status != http.StatusBadRequest {
			t.Errorf("%s = %d", body, got.status)
		}
	}
	if !measured() {
		t.Fatal("history deleted without confirmation")
	}
	if got := h.call(http.MethodPost, "/api/history/delete", `{"confirm":"APAGAR"}`); got.status != http.StatusOK {
		t.Fatalf("%d %s", got.status, got.body)
	}
	if measured() {
		t.Error("history still there")
	}
}

func TestAutostart(t *testing.T) {
	h := newHarness(t)
	h.login()
	if got := h.call(http.MethodPut, "/api/autostart", `{"enabled":true}`); got.status != http.StatusOK || !h.autostart.on {
		t.Errorf("%d on=%v", got.status, h.autostart.on)
	}
	h.autostart.err = ErrAutostartUnavailable
	if got := h.call(http.MethodPut, "/api/autostart", `{"enabled":true}`); got.status != http.StatusConflict {
		t.Errorf("unavailable: %d", got.status)
	}
}

func TestNewRejectsIncompleteDependencies(t *testing.T) {
	if _, err := New(&Deps{}); err == nil {
		t.Error("want an error")
	}
}

// Package web serves the panel: a page and a small JSON API that only the
// user's own browser can reach. See the plan, section 10.
package web

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/Reinaldo-Porfirio/avanet/internal/agent"
	"github.com/Reinaldo-Porfirio/avanet/internal/analysis"
	"github.com/Reinaldo-Porfirio/avanet/internal/domain"
	"github.com/Reinaldo-Porfirio/avanet/internal/report"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 3 * time.Second
	maxHeaderBytes    = 16 << 10
)

// ErrAutostartUnavailable means this copy of the program cannot be registered
// to start with Windows (for example, a temporary development build).
var ErrAutostartUnavailable = errors.New("autostart unavailable for this executable")

// Agent is what the panel needs from the running monitor.
type Agent interface {
	Status() agent.Status
	RequestSpeedTest()
	ApplySettings(profile *domain.Profile, budget domain.DataBudget) error
}

// Reports gives access to the daily reports.
type Reports interface {
	Load(ctx context.Context, day domain.Day, now time.Time) (*analysis.Daily, error)
	Days(ctx context.Context, now time.Time) ([]report.DayEntry, error)
	Insights(ctx context.Context, until domain.Day, now time.Time, planDownload, planUpload float64) (*analysis.Insights, error)
}

// Store is the persistence the panel needs (implemented by *storage.DB).
type Store interface {
	Preferences(ctx context.Context) (domain.Preferences, []string, error)
	SavePreferences(ctx context.Context, p domain.Preferences, at time.Time) error
	Setting(ctx context.Context, key string) (string, bool, error)
	DataUsage(ctx context.Context, day domain.Day, class domain.DataClass) (int64, error)
	Coverage(ctx context.Context, from, to time.Time) (time.Duration, error)
	LatestSpeedTest(ctx context.Context) (domain.SpeedResult, storageTrigger, bool, error)
	DeleteHistory(ctx context.Context, today domain.Day) error
	Compact(ctx context.Context) error
}

// Autostart turns "start with Windows" on and off.
type Autostart interface {
	Enabled() (bool, error)
	SetEnabled(on bool) error
}

// Deps are the server's collaborators.
type Deps struct {
	Agent     Agent
	Reports   Reports
	Store     Store
	Autostart Autostart
	Assets    fs.FS // the panel's files
	Logger    *slog.Logger
	Now       func() time.Time // nil means time.Now
}

// Server is the panel's HTTP server. Create it with New, then call Listen and
// Serve.
type Server struct {
	deps     Deps
	tickets  tickets
	session  string
	listener net.Listener
	http     *http.Server
	// Known after Listen: the only accepted Host and the cookie's name. The
	// name carries the port because browsers share cookies between ports.
	host       string
	cookieName string
}

// New checks the dependencies and creates the server's secrets.
func New(deps *Deps) (*Server, error) {
	if deps.Agent == nil || deps.Reports == nil || deps.Store == nil || deps.Autostart == nil || deps.Assets == nil || deps.Logger == nil {
		return nil, errors.New("web server needs every dependency")
	}
	d := *deps
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Server{deps: d, tickets: tickets{now: d.Now}, session: rand.Text()}, nil
}

// Listen reserves a random port on the loopback address, which other
// computers cannot reach.
func (s *Server) Listen(ctx context.Context) error {
	var config net.ListenConfig
	listener, err := config.Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen on loopback: %w", err)
	}
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return errors.Join(errors.New("listener has no TCP address"), listener.Close())
	}

	s.listener = listener
	s.host = listener.Addr().String()
	s.cookieName = "avanet_session_" + strconv.Itoa(address.Port)
	s.http = &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(s.deps.Logger.Handler(), slog.LevelWarn),
	}
	return nil
}

// Serve answers requests until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	stopped := make(chan error, 1)
	go func() {
		<-ctx.Done()
		// ctx is already cancelled here; the shutdown needs a deadline of its own.
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		stopped <- s.http.Shutdown(shutdown)
	}()

	if err := s.http.Serve(s.listener); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve panel: %w", err)
	}
	return <-stopped
}

// OpenURL returns a link that opens the panel once, within ttl.
func (s *Server) OpenURL(ttl time.Duration) string {
	return "http://" + s.host + "/open?ticket=" + s.tickets.issue(ttl)
}

func (s *Server) routes() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/now", s.handleNow)
	api.HandleFunc("GET /api/reports", s.handleReports)
	api.HandleFunc("GET /api/reports/{day}", s.handleReport)
	api.HandleFunc("GET /api/insights", s.handleInsights)
	api.HandleFunc("GET /api/settings", s.handleSettings)
	api.HandleFunc("PUT /api/settings", s.handleSaveSettings)
	api.HandleFunc("PUT /api/autostart", s.handleAutostart)
	api.HandleFunc("POST /api/speedtest", s.handleSpeedTest)
	api.HandleFunc("POST /api/history/delete", s.handleDeleteHistory)
	api.HandleFunc("GET /{path...}", s.handleAsset)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /open", s.handleOpen)
	mux.Handle("/", s.withSession(s.withSameOrigin(api)))
	return s.withSecurityHeaders(s.withHostCheck(mux))
}

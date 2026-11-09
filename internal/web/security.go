package web

import (
	"crypto/rand"
	"crypto/subtle"
	"net/http"
	"sync"
	"time"
)

const (
	// maxTickets bounds the links waiting to be opened; the oldest goes first.
	maxTickets = 8
	// contentSecurityPolicy allows only this server's own files. Nothing is
	// inline and nothing comes from the internet.
	contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; " +
		"img-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
)

// tickets are single-use links that open the panel. The session itself lives
// in a cookie, so what stays in the browser's history is a dead link.
type tickets struct {
	mu    sync.Mutex
	now   func() time.Time
	items []ticket
}

type ticket struct {
	value   string
	expires time.Time
}

func (t *tickets) issue(ttl time.Duration) string {
	value := rand.Text()

	t.mu.Lock()
	defer t.mu.Unlock()
	t.items = append(t.items, ticket{value: value, expires: t.now().Add(ttl)})
	if len(t.items) > maxTickets {
		t.items = t.items[len(t.items)-maxTickets:]
	}
	return value
}

// redeem consumes a ticket and reports whether it was valid.
func (t *tickets) redeem(value string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	valid := false
	kept := t.items[:0]
	for _, item := range t.items {
		matches := equal(item.value, value)
		if matches && now.Before(item.expires) {
			valid = true
			continue
		}
		if !matches {
			kept = append(kept, item)
		}
	}
	t.items = kept
	return valid
}

// equal compares secrets in constant time, so the time a comparison takes
// says nothing about how much of a guess was right.
func equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// withSecurityHeaders sets the headers every response carries.
func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// withHostCheck blocks DNS rebinding, where a malicious site makes its own
// domain resolve to 127.0.0.1 to reach this server from the user's browser.
func (s *Server) withHostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.host {
			http.Error(w, "Endereço não permitido.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withSession refuses every request without the session cookie.
func (s *Server) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.cookieName)
		if err != nil || !equal(cookie.Value, s.session) {
			http.Error(w, "Abra o painel pelo ícone do AvaNet, perto do relógio do Windows.", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withSameOrigin blocks cross-site requests that try to change something.
// Browsers always send Origin on these methods, so a missing one is refused.
func (s *Server) withSameOrigin(next http.Handler) http.Handler {
	origin := "http://" + s.host
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("Origin") != origin {
			http.Error(w, "Origem não permitida.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleOpen exchanges a ticket for the session cookie.
func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	if !s.tickets.redeem(r.URL.Query().Get("ticket")) {
		http.Error(w, "Este link já foi usado ou expirou. Abra o painel de novo pelo ícone do AvaNet.", http.StatusUnauthorized)
		return
	}
	// Not marked Secure: this is plain HTTP on the loopback address, whose
	// traffic never leaves the computer, and without a domain name there is
	// no certificate to validate.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: see above; HttpOnly and SameSite are set
		Name: s.cookieName, Value: s.session, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

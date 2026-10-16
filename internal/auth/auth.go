package auth

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// SessionCookieName is the name of the signed session cookie.
	SessionCookieName = "northscope_session"
	// DefaultSessionTTL is how long a sign-in stays valid.
	DefaultSessionTTL = 12 * time.Hour

	LoginPath  = "/login"
	LogoutPath = "/logout"

	maxLoginFormBytes = 8 << 10

	invalidCredentialsMessage = "Invalid username or password."
	tooManyAttemptsMessage    = "Too many failed sign-in attempts. Try again later."
)

var (
	//go:embed login.html
	loginHTML string
	//go:embed login.css
	loginCSSSource string
	// Browsers hash a <style> element after turning CRLF into LF, so a
	// checkout with Windows line endings must not change the hashed bytes.
	loginCSS = strings.ReplaceAll(loginCSSSource, "\r\n", "\n")

	loginTemplate = template.Must(template.New("login").Parse(loginHTML))
	// loginCSP allows only the login page's own inline stylesheet, by hash.
	loginCSP = "default-src 'none'; style-src '" + cssHash(loginCSS) + "'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
)

// Options configure an Authenticator.
type Options struct {
	CookieSecure CookieSecureMode
	// ProtectMetrics requires a session for /metrics. Prometheus cannot sign
	// in, so leave it off unless metrics are scraped some other way.
	ProtectMetrics bool
	// SessionTTL defaults to DefaultSessionTTL.
	SessionTTL time.Duration
	// Now is overridable for tests.
	Now func() time.Time
}

// Authenticator guards NorthScope with a single username and password.
type Authenticator struct {
	username       string
	usernameDigest [sha256.Size]byte
	passwordDigest [sha256.Size]byte
	signer         sessionSigner
	throttle       *loginThrottle
	cookieSecure   CookieSecureMode
	protectMetrics bool
	now            func() time.Time
}

// New returns an Authenticator for the given credentials.
func New(creds Credentials, opts Options) (*Authenticator, error) {
	if err := creds.Validate(); err != nil {
		return nil, err
	}
	ttl := opts.SessionTTL
	if ttl == 0 {
		ttl = DefaultSessionTTL
	}
	if ttl < time.Minute {
		return nil, errors.New("auth session TTL must be at least one minute")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	return &Authenticator{
		username:       creds.Username,
		usernameDigest: sha256.Sum256([]byte(creds.Username)),
		passwordDigest: sha256.Sum256([]byte(creds.Password)),
		signer:         newSessionSigner(creds, ttl),
		throttle:       newLoginThrottle(loginBurst, loginRefill),
		cookieSecure:   opts.CookieSecure,
		protectMetrics: opts.ProtectMetrics,
		now:            now,
	}, nil
}

// Username returns the configured login name.
func (a *Authenticator) Username() string {
	return a.username
}

// Middleware requires a valid session for every request except the login
// page, logout, health probes, and (unless ProtectMetrics is set) /metrics.
// Unauthenticated API, metrics, WebSocket, and non-GET requests get 401 JSON;
// browser navigation is redirected to the login page.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.isPublic(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if session, ok := a.sessionFromRequest(r); ok {
			next.ServeHTTP(w, r.WithContext(withSession(r.Context(), session)))
			return
		}

		if requiresStatusResponse(r) || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			writeUnauthorized(w)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, loginURL(r.URL.RequestURI()), http.StatusFound)
	})
}

func (a *Authenticator) isPublic(path string) bool {
	switch path {
	case LoginPath, LogoutPath, "/healthz", "/readyz":
		return true
	case "/metrics":
		return !a.protectMetrics
	}
	return false
}

func requiresStatusResponse(r *http.Request) bool {
	path := r.URL.Path
	return path == "/api" || strings.HasPrefix(path, "/api/") ||
		path == "/ws" || strings.HasPrefix(path, "/ws/") ||
		path == "/metrics" ||
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: "authentication required"})
}

// LoginHandler serves GET and POST /login.
func (a *Authenticator) LoginHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			next := safeNext(r.URL.Query().Get("next"))
			if _, ok := a.sessionFromRequest(r); ok {
				http.Redirect(w, r, next, http.StatusSeeOther)
				return
			}
			renderLogin(w, http.StatusOK, next, "")
		case http.MethodPost:
			a.handleLoginSubmit(w, r)
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
	})
}

func (a *Authenticator) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin sign-in rejected", http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxLoginFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid sign-in form", http.StatusBadRequest)
		return
	}
	next := safeNext(r.PostForm.Get("next"))

	client := clientKey(r)
	now := a.now()
	if ok, wait := a.throttle.allow(client, now); !ok {
		log.Printf("auth: sign-in throttled for client %s", client)
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		renderLogin(w, http.StatusTooManyRequests, next, tooManyAttemptsMessage)
		return
	}

	if !a.credentialsMatch(r.PostForm.Get("username"), r.PostForm.Get("password")) {
		a.throttle.fail(client, now)
		log.Printf("auth: failed sign-in from client %s", client)
		renderLogin(w, http.StatusUnauthorized, next, invalidCredentialsMessage)
		return
	}

	token, expiresAt, err := a.signer.issue(now)
	if err != nil {
		log.Printf("auth: issue session: %v", err)
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.sessionCookie(r, token, expiresAt))
	log.Printf("auth: %s signed in from client %s", a.username, client)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// credentialsMatch compares SHA-256 digests in constant time, so neither the
// length nor the content of the configured credentials leaks through timing.
// Both comparisons always run.
func (a *Authenticator) credentialsMatch(username, password string) bool {
	usernameDigest := sha256.Sum256([]byte(username))
	passwordDigest := sha256.Sum256([]byte(password))
	usernameOK := subtle.ConstantTimeCompare(usernameDigest[:], a.usernameDigest[:])
	passwordOK := subtle.ConstantTimeCompare(passwordDigest[:], a.passwordDigest[:])
	return usernameOK&passwordOK == 1
}

// LogoutHandler serves POST /logout. It clears the session cookie and returns
// to the login page.
func (a *Authenticator) LogoutHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin sign-out rejected", http.StatusForbidden)
			return
		}
		http.SetCookie(w, a.sessionCookie(r, "", time.Unix(0, 0)))
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, LoginPath, http.StatusSeeOther)
	})
}

func (a *Authenticator) sessionFromRequest(r *http.Request) (Session, bool) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return Session{}, false
	}
	expiresAt, err := a.signer.verify(cookie.Value, a.now())
	if err != nil {
		return Session{}, false
	}
	return Session{Username: a.username, ExpiresAt: expiresAt}, true
}

func (a *Authenticator) sessionCookie(r *http.Request, value string, expiresAt time.Time) *http.Cookie {
	maxAge := int(expiresAt.Sub(a.now()).Seconds())
	if maxAge <= 0 {
		maxAge = -1
	}
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   a.secureCookie(r),
		SameSite: http.SameSiteLaxMode,
	}
}

func (a *Authenticator) secureCookie(r *http.Request) bool {
	switch a.cookieSecure {
	case CookieSecureAlways:
		return true
	case CookieSecureNever:
		return false
	}
	if r.TLS != nil {
		return true
	}
	proto, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ",")
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

func renderLogin(w http.ResponseWriter, status int, next, message string) {
	var body bytes.Buffer
	if err := loginTemplate.Execute(&body, struct {
		CSS   template.CSS
		Next  string
		Error string
	}{CSS: template.CSS(loginCSS), Next: next, Error: message}); err != nil {
		log.Printf("auth: render login page: %v", err)
		http.Error(w, "could not render sign-in page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Security-Policy", loginCSP)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

func cssHash(css string) string {
	sum := sha256.Sum256([]byte(css))
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}

// loginURL returns the login page URL that returns to target after sign-in.
func loginURL(target string) string {
	target = safeNext(target)
	if target == "/" {
		return LoginPath
	}
	return LoginPath + "?next=" + url.QueryEscape(target)
}

// safeNext only allows local absolute paths as post-login redirects, so the
// login form cannot be used as an open redirect.
func safeNext(next string) string {
	if next == "" || next[0] != '/' || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n\t") {
		return "/"
	}
	parsed, err := url.Parse(next)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.Path == LoginPath || parsed.Path == LogoutPath {
		return "/"
	}
	return next
}

// sameOrigin rejects browser form posts from other sites. Browsers that send
// Sec-Fetch-Site are trusted on it, because with Referrer-Policy: no-referrer
// they send "Origin: null" even for same-origin form posts. Otherwise a real
// Origin must match the host; requests without one, such as curl, are allowed.
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// clientKey identifies the client for login throttling. Forwarded headers are
// ignored because clients can forge them; behind a proxy every client shares
// the proxy's address.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

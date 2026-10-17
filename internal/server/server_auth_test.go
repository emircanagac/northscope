package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	"github.com/emircanagac/northscope/internal/auth"
	"github.com/emircanagac/northscope/internal/k8s"
	"k8s.io/client-go/kubernetes/fake"
)

var testCredentials = auth.Credentials{
	Username:   "admin",
	Password:   "test-password",
	SessionKey: strings.Repeat("s", auth.MinSessionKeyLength),
}

func newAuthTestServer(t *testing.T, opts ...Option) (*Server, *k8s.Watcher) {
	t.Helper()
	watcher, err := k8s.NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	return New(":0", watcher, fstest.MapFS{
		"index.html": {Data: []byte(`<div id="root"></div>`)},
	}, "test", opts...), watcher
}

func newTestAuthenticator(t *testing.T, opts auth.Options) *auth.Authenticator {
	t.Helper()
	authenticator, err := auth.New(testCredentials, opts)
	if err != nil {
		t.Fatalf("create authenticator: %v", err)
	}
	return authenticator
}

func serveRequest(server *Server, request *http.Request) *http.Response {
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, request)
	return recorder.Result()
}

func signIn(t *testing.T, server *Server) *http.Cookie {
	t.Helper()
	form := url.Values{"username": {testCredentials.Username}, "password": {testCredentials.Password}}
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := serveRequest(server, request)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign in: expected 303, got %d", response.StatusCode)
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == auth.SessionCookieName {
			return cookie
		}
	}
	t.Fatal("sign in did not set a session cookie")
	return nil
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	disabled, _ := newAuthTestServer(t)
	enabled, _ := newAuthTestServer(t, WithAuth(newTestAuthenticator(t, auth.Options{})))

	for name, tt := range map[string]struct {
		server *Server
		path   string
	}{
		"disabled ui":      {disabled, "/"},
		"disabled api":     {disabled, "/api/version"},
		"disabled health":  {disabled, "/healthz"},
		"enabled redirect": {enabled, "/"},
		"enabled 401":      {enabled, "/api/topology"},
		"enabled health":   {enabled, "/healthz"},
		"enabled login":    {enabled, "/login"},
	} {
		response := serveRequest(tt.server, httptest.NewRequest(http.MethodGet, tt.path, nil))
		header := response.Header
		if csp := header.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Fatalf("%s: missing frame-ancestors in CSP %q", name, csp)
		}
		if tt.path != "/login" && header.Get("Content-Security-Policy") != contentSecurityPolicy {
			t.Fatalf("%s: unexpected CSP %q", name, header.Get("Content-Security-Policy"))
		}
		if header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Referrer-Policy") != "no-referrer" || header.Get("X-Frame-Options") != "DENY" {
			t.Fatalf("%s: missing security headers: %v", name, header)
		}
	}
}

func TestAuthDisabledKeepsRoutesOpen(t *testing.T) {
	server, _ := newAuthTestServer(t)

	for _, path := range []string{"/", "/api/topology", "/api/version", "/metrics", "/healthz"} {
		if response := serveRequest(server, httptest.NewRequest(http.MethodGet, path, nil)); response.StatusCode >= 300 && path != "/healthz" {
			t.Fatalf("%s: expected open route, got %d", path, response.StatusCode)
		}
	}

	response := serveRequest(server, httptest.NewRequest(http.MethodGet, "/api/session", nil))
	var session map[string]any
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if response.StatusCode != http.StatusOK || session["authEnabled"] != false || len(session) != 1 {
		t.Fatalf("unexpected disabled session response %d %v", response.StatusCode, session)
	}
	if cacheControl := response.Header.Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("expected no-store, got %q", cacheControl)
	}

	// Without auth there is no login route; the SPA fallback serves the UI.
	if response := serveRequest(server, httptest.NewRequest(http.MethodPost, "/logout", nil)); response.StatusCode == http.StatusSeeOther {
		t.Fatal("logout must not be handled when authentication is disabled")
	}
}

func TestAuthEnabledProtectsRoutes(t *testing.T) {
	server, _ := newAuthTestServer(t, WithAuth(newTestAuthenticator(t, auth.Options{})))

	for path, want := range map[string]int{
		"/":             http.StatusFound,
		"/api/topology": http.StatusUnauthorized,
		"/api/version":  http.StatusUnauthorized,
		"/api/session":  http.StatusUnauthorized,
		"/metrics":      http.StatusOK,
		"/healthz":      http.StatusNoContent,
		"/readyz":       http.StatusServiceUnavailable,
		"/login":        http.StatusOK,
	} {
		if response := serveRequest(server, httptest.NewRequest(http.MethodGet, path, nil)); response.StatusCode != want {
			t.Fatalf("%s: expected %d, got %d", path, want, response.StatusCode)
		}
	}

	cookie := signIn(t, server)
	for _, path := range []string{"/", "/api/topology", "/api/version"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(cookie)
		if response := serveRequest(server, request); response.StatusCode != http.StatusOK {
			t.Fatalf("%s: expected 200 with a session, got %d", path, response.StatusCode)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	request.AddCookie(cookie)
	response := serveRequest(server, request)
	var session struct {
		AuthEnabled bool      `json:"authEnabled"`
		Username    string    `json:"username"`
		ExpiresAt   time.Time `json:"expiresAt"`
	}
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if !session.AuthEnabled || session.Username != "admin" || time.Until(session.ExpiresAt) < auth.DefaultSessionTTL-time.Minute {
		t.Fatalf("unexpected session response %+v", session)
	}
}

func TestAuthProtectMetricsRequiresSession(t *testing.T) {
	server, _ := newAuthTestServer(t, WithAuth(newTestAuthenticator(t, auth.Options{ProtectMetrics: true})))

	if response := serveRequest(server, httptest.NewRequest(http.MethodGet, "/metrics", nil)); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected protected metrics to require a session, got %d", response.StatusCode)
	}
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.AddCookie(signIn(t, server))
	if response := serveRequest(server, request); response.StatusCode != http.StatusOK {
		t.Fatalf("expected metrics with a session, got %d", response.StatusCode)
	}
}

func TestWebSocketRequiresSessionBeforeUpgrade(t *testing.T) {
	server, watcher := newAuthTestServer(t, WithAuth(newTestAuthenticator(t, auth.Options{})))
	httpServer := httptest.NewServer(server.httpServer.Handler)
	defer httpServer.Close()
	wsBase := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	for _, path := range []string{"/ws", "/ws/topology"} {
		conn, response, err := websocket.DefaultDialer.Dial(wsBase+path, nil)
		if conn != nil {
			_ = conn.Close()
		}
		if !errors.Is(err, websocket.ErrBadHandshake) || response == nil || response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401 handshake rejection, got err %v, response %+v", path, err, response)
		}
	}
	if got := watcher.Metrics().WebsocketSubscribers; got != 0 {
		t.Fatalf("rejected handshakes must not subscribe, got %d subscribers", got)
	}

	header := http.Header{}
	header.Set("Cookie", signIn(t, server).String())
	conn, _, err := websocket.DefaultDialer.Dial(wsBase+"/ws", header)
	if err != nil {
		t.Fatalf("expected websocket with a session to connect: %v", err)
	}
	_ = conn.Close()
}

func TestWebSocketClosesWhenSessionExpires(t *testing.T) {
	// Issue sessions from a clock that runs one TTL minus two seconds behind,
	// so the session is valid now but expires two seconds from now.
	ttl := time.Minute
	authenticator := newTestAuthenticator(t, auth.Options{
		SessionTTL: ttl,
		Now:        func() time.Time { return time.Now().Add(2*time.Second - ttl) },
	})
	server, _ := newAuthTestServer(t, WithAuth(authenticator))
	httpServer := httptest.NewServer(server.httpServer.Handler)
	defer httpServer.Close()

	header := http.Header{}
	header.Set("Cookie", signIn(t, server).String())
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http")+"/ws", header)
	if err != nil {
		t.Fatalf("connect websocket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, _, err := conn.ReadMessage()
		if err == nil {
			continue
		}
		var closeErr *websocket.CloseError
		if !errors.As(err, &closeErr) || closeErr.Code != websocket.ClosePolicyViolation || closeErr.Text != "session expired" {
			t.Fatalf("expected session expired close, got %v", err)
		}
		return
	}
}

package auth

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var testCredentials = Credentials{
	Username:   "admin",
	Password:   "correct horse battery staple",
	SessionKey: strings.Repeat("k", MinSessionKeyLength),
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestAuthenticator(t *testing.T, opts Options) (*Authenticator, *fakeClock) {
	t.Helper()
	clock := newFakeClock()
	if opts.Now == nil {
		opts.Now = clock.Now
	}
	authenticator, err := New(testCredentials, opts)
	if err != nil {
		t.Fatalf("create authenticator: %v", err)
	}
	return authenticator, clock
}

// testHandler mimics the server: login and logout routes behind the middleware
// in front of a handler that echoes the verified session.
func testHandler(a *Authenticator) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(LoginPath, a.LoginHandler())
	mux.Handle(LogoutPath, a.LogoutHandler())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if session, ok := SessionFromContext(r.Context()); ok {
			w.Header().Set("X-Session-User", session.Username)
		}
		_, _ = io.WriteString(w, "protected")
	})
	return a.Middleware(mux)
}

func postLogin(t *testing.T, handler http.Handler, form url.Values, modify func(*http.Request)) *http.Response {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, LoginPath, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "192.0.2.10:51234"
	if modify != nil {
		modify(request)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Result()
}

func validLogin() url.Values {
	return url.Values{"username": {testCredentials.Username}, "password": {testCredentials.Password}}
}

func sessionCookieFrom(t *testing.T, response *http.Response) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == SessionCookieName {
			return cookie
		}
	}
	t.Fatalf("response has no %s cookie", SessionCookieName)
	return nil
}

func get(handler http.Handler, path string, cookie *http.Cookie) *http.Response {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Result()
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

func writeAuthFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestLoadCredentialsReadsSecretFilesAndTrimsOneTrailingNewline(t *testing.T) {
	dir := writeAuthFiles(t, map[string]string{
		UsernameFile:   "admin\n",
		PasswordFile:   "s3cret value \r\n",
		SessionKeyFile: testCredentials.SessionKey,
	})

	creds, err := LoadCredentials(dir)
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}
	if creds.Username != "admin" || creds.Password != "s3cret value " || creds.SessionKey != testCredentials.SessionKey {
		t.Fatalf("unexpected credentials: %+v", creds)
	}
}

func TestLoadCredentialsRejectsMissingEmptyAndWeakFiles(t *testing.T) {
	complete := map[string]string{
		UsernameFile:   "admin",
		PasswordFile:   "password",
		SessionKeyFile: testCredentials.SessionKey,
	}
	tests := map[string]struct {
		override map[string]string
		omit     string
		want     string
	}{
		"missing username":    {omit: UsernameFile, want: "username"},
		"missing password":    {omit: PasswordFile, want: "password"},
		"missing session key": {omit: SessionKeyFile, want: "session-key"},
		"empty password":      {override: map[string]string{PasswordFile: "\n"}, want: "is empty"},
		"empty username":      {override: map[string]string{UsernameFile: ""}, want: "is empty"},
		"short session key":   {override: map[string]string{SessionKeyFile: "too-short"}, want: "at least 32"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{}
			for key, value := range complete {
				if key != tt.omit {
					files[key] = value
				}
			}
			for key, value := range tt.override {
				files[key] = value
			}
			_, err := LoadCredentials(writeAuthFiles(t, files))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}

	if _, err := LoadCredentials(""); err == nil {
		t.Fatal("expected an empty directory flag to be rejected")
	}
}

func TestParseCookieSecureMode(t *testing.T) {
	for input, want := range map[string]CookieSecureMode{
		"":      CookieSecureAuto,
		"auto":  CookieSecureAuto,
		"TRUE":  CookieSecureAlways,
		"false": CookieSecureNever,
	} {
		got, err := ParseCookieSecureMode(input)
		if err != nil || got != want {
			t.Fatalf("ParseCookieSecureMode(%q) = %v, %v; want %v", input, got, err, want)
		}
	}
	if _, err := ParseCookieSecureMode("sometimes"); err == nil {
		t.Fatal("expected invalid mode to be rejected")
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	if _, err := New(Credentials{Username: "admin", Password: "x", SessionKey: "short"}, Options{}); err == nil {
		t.Fatal("expected short session key to be rejected")
	}
	if _, err := New(testCredentials, Options{SessionTTL: time.Second}); err == nil {
		t.Fatal("expected a sub-minute session TTL to be rejected")
	}
}

func TestCredentialsMatchComparesDigestsOfAnyLength(t *testing.T) {
	authenticator, _ := newTestAuthenticator(t, Options{})
	tests := []struct {
		name     string
		username string
		password string
		want     bool
	}{
		{"correct", testCredentials.Username, testCredentials.Password, true},
		{"wrong password", testCredentials.Username, "wrong", false},
		{"wrong username", "root", testCredentials.Password, false},
		{"password prefix", testCredentials.Username, testCredentials.Password[:5], false},
		{"password with suffix", testCredentials.Username, testCredentials.Password + "x", false},
		{"long input", strings.Repeat("a", 4096), strings.Repeat("b", 4096), false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		if got := authenticator.credentialsMatch(tt.username, tt.password); got != tt.want {
			t.Fatalf("%s: credentialsMatch = %t, want %t", tt.name, got, tt.want)
		}
	}
}

func TestLoginPageRendersFormWithRestrictivePolicy(t *testing.T) {
	authenticator, _ := newTestAuthenticator(t, Options{})
	response := get(testHandler(authenticator), "/login?next=%2Fapi%2Fversion", nil)
	body := readBody(t, response)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	if !strings.Contains(body, `<form method="post" action="/login">`) || !strings.Contains(body, `value="/api/version"`) {
		t.Fatalf("login form missing or next not preserved:\n%s", body)
	}
	if csp := response.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, cssHash(loginCSS)) || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("unexpected login CSP %q", csp)
	}
	if strings.Contains(loginCSS, "\r") || !strings.Contains(body, "<style>"+loginCSS+"</style>") {
		t.Fatal("inline login stylesheet must match the hash in the CSP byte for byte")
	}
	if cacheControl := response.Header.Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("expected no-store, got %q", cacheControl)
	}
}

func TestLoginSuccessSetsSignedSessionCookie(t *testing.T) {
	authenticator, clock := newTestAuthenticator(t, Options{})
	handler := testHandler(authenticator)

	form := validLogin()
	form.Set("next", "/?namespace=demo")
	response := postLogin(t, handler, form, nil)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/?namespace=demo" {
		t.Fatalf("expected redirect to next, got %q", location)
	}

	cookie := sessionCookieFrom(t, response)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("unexpected cookie attributes: %+v", cookie)
	}
	if cookie.Secure {
		t.Fatal("cookie must not be Secure for plain HTTP in auto mode")
	}
	if cookie.MaxAge != int(DefaultSessionTTL.Seconds()) || !cookie.Expires.Equal(clock.Now().Add(DefaultSessionTTL)) {
		t.Fatalf("unexpected cookie lifetime: max-age %d, expires %s", cookie.MaxAge, cookie.Expires)
	}

	protected := get(handler, "/", cookie)
	if protected.StatusCode != http.StatusOK || protected.Header.Get("X-Session-User") != "admin" {
		t.Fatalf("expected session to unlock protected routes, got %d", protected.StatusCode)
	}
}

func TestLoginFailureReturnsGenericError(t *testing.T) {
	authenticator, _ := newTestAuthenticator(t, Options{})
	handler := testHandler(authenticator)

	for _, form := range []url.Values{
		{"username": {"admin"}, "password": {"wrong"}},
		{"username": {"nobody"}, "password": {testCredentials.Password}},
		{},
	} {
		response := postLogin(t, handler, form, nil)
		body := readBody(t, response)
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", response.StatusCode)
		}
		if !strings.Contains(body, invalidCredentialsMessage) {
			t.Fatalf("expected generic error message, got:\n%s", body)
		}
		for _, cookie := range response.Cookies() {
			if cookie.Name == SessionCookieName {
				t.Fatal("failed login must not set a session cookie")
			}
		}
	}
}

func TestLoginRejectsUnsafeRedirectTargets(t *testing.T) {
	authenticator, _ := newTestAuthenticator(t, Options{})
	handler := testHandler(authenticator)

	for _, next := range []string{"https://evil.example", "//evil.example", "/\\evil.example", "javascript:alert(1)", "/login", "/logout"} {
		form := validLogin()
		form.Set("next", next)
		response := postLogin(t, handler, form, nil)
		if location := response.Header.Get("Location"); location != "/" {
			t.Fatalf("next %q redirected to %q, want /", next, location)
		}
	}
}

func TestLoginRejectsCrossOriginPost(t *testing.T) {
	tests := map[string]struct {
		header map[string]string
		want   int
	}{
		"foreign origin":               {map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		"cross-site fetch":             {map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "null"}, http.StatusForbidden},
		"same-site subdomain":          {map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "https://other.internal"}, http.StatusForbidden},
		"matching origin":              {map[string]string{"Origin": "http://northscope.internal"}, http.StatusSeeOther},
		"same-origin with no-referrer": {map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "null"}, http.StatusSeeOther},
		"no browser headers":           {nil, http.StatusSeeOther},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			authenticator, _ := newTestAuthenticator(t, Options{})
			response := postLogin(t, testHandler(authenticator), validLogin(), func(r *http.Request) {
				r.Host = "northscope.internal"
				for key, value := range tt.header {
					r.Header.Set(key, value)
				}
			})
			if response.StatusCode != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, response.StatusCode)
			}
		})
	}
}

func TestLoginWhileSignedInRedirectsToNext(t *testing.T) {
	authenticator, _ := newTestAuthenticator(t, Options{})
	handler := testHandler(authenticator)
	cookie := sessionCookieFrom(t, postLogin(t, handler, validLogin(), nil))

	response := get(handler, "/login?next=%2Fdocs", cookie)
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/docs" {
		t.Fatalf("expected redirect to /docs, got %d %q", response.StatusCode, response.Header.Get("Location"))
	}
}

func TestSessionCookieSecureModes(t *testing.T) {
	tests := []struct {
		name   string
		mode   CookieSecureMode
		modify func(*http.Request)
		want   bool
	}{
		{"auto over http", CookieSecureAuto, nil, false},
		{"auto over tls", CookieSecureAuto, func(r *http.Request) { r.TLS = &tls.ConnectionState{} }, true},
		{"auto behind https proxy", CookieSecureAuto, func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") }, true},
		{"auto with proxy chain", CookieSecureAuto, func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "HTTPS, http") }, true},
		{"auto behind http proxy", CookieSecureAuto, func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "http") }, false},
		{"always", CookieSecureAlways, nil, true},
		{"never over tls", CookieSecureNever, func(r *http.Request) { r.TLS = &tls.ConnectionState{} }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authenticator, _ := newTestAuthenticator(t, Options{CookieSecure: tt.mode})
			cookie := sessionCookieFrom(t, postLogin(t, testHandler(authenticator), validLogin(), tt.modify))
			if cookie.Secure != tt.want {
				t.Fatalf("Secure = %t, want %t", cookie.Secure, tt.want)
			}
		})
	}
}

func TestSessionExpiresAfterTTL(t *testing.T) {
	authenticator, clock := newTestAuthenticator(t, Options{SessionTTL: time.Hour})
	handler := testHandler(authenticator)
	cookie := sessionCookieFrom(t, postLogin(t, handler, validLogin(), nil))

	clock.Advance(59 * time.Minute)
	if response := get(handler, "/api/topology", cookie); response.StatusCode != http.StatusOK {
		t.Fatalf("expected session to be valid before expiry, got %d", response.StatusCode)
	}

	clock.Advance(time.Minute)
	if response := get(handler, "/api/topology", cookie); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected expired session to be rejected, got %d", response.StatusCode)
	}
}

func TestSessionRejectsTamperedAndForeignTokens(t *testing.T) {
	clock := newFakeClock()
	signer := newSessionSigner(testCredentials, time.Hour)
	token, _, err := signer.issue(clock.Now())
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	if _, err := signer.verify(token, clock.Now()); err != nil {
		t.Fatalf("expected issued token to verify: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 4 {
		t.Fatalf("unexpected token format %q", token)
	}
	extended := strconv.FormatInt(clock.Now().Add(30*24*time.Hour).Unix(), 10)
	flipped := []byte(parts[3])
	flipped[0] ^= 1

	rotatedPassword := testCredentials
	rotatedPassword.Password = "rotated"
	rotatedKey := testCredentials
	rotatedKey.SessionKey = strings.Repeat("r", MinSessionKeyLength)

	tests := map[string]struct {
		signer sessionSigner
		token  string
	}{
		"extended expiry":     {signer, strings.Join([]string{parts[0], extended, parts[2], parts[3]}, ".")},
		"changed nonce":       {signer, strings.Join([]string{parts[0], parts[1], "AAAAAAAAAAAAAAAAAAAAAA", parts[3]}, ".")},
		"flipped signature":   {signer, strings.Join([]string{parts[0], parts[1], parts[2], string(flipped)}, ".")},
		"missing signature":   {signer, strings.Join(parts[:3], ".")},
		"garbage":             {signer, "not-a-session"},
		"empty":               {signer, ""},
		"rotated password":    {newSessionSigner(rotatedPassword, time.Hour), token},
		"rotated session key": {newSessionSigner(rotatedKey, time.Hour), token},
		"lifetime beyond TTL": {sessionSigner{key: signer.key, ttl: time.Minute}, token},
		"changed version":     {signer, "v2." + strings.Join(parts[1:], ".")},
	}
	for name, tt := range tests {
		if _, err := tt.signer.verify(tt.token, clock.Now()); err == nil {
			t.Fatalf("%s: expected token to be rejected", name)
		}
	}
}

func TestMiddlewareRouteClasses(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		header         map[string]string
		protectMetrics bool
		wantStatus     int
		wantLocation   string
	}{
		{name: "login page", method: http.MethodGet, path: "/login", wantStatus: http.StatusOK},
		{name: "liveness", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK},
		{name: "readiness", method: http.MethodGet, path: "/readyz", wantStatus: http.StatusOK},
		{name: "metrics open by default", method: http.MethodGet, path: "/metrics", wantStatus: http.StatusOK},
		{name: "metrics protected", method: http.MethodGet, path: "/metrics", protectMetrics: true, wantStatus: http.StatusUnauthorized},
		{name: "api", method: http.MethodGet, path: "/api/topology", wantStatus: http.StatusUnauthorized},
		{name: "api version", method: http.MethodGet, path: "/api/version", wantStatus: http.StatusUnauthorized},
		{name: "api session", method: http.MethodGet, path: "/api/session", wantStatus: http.StatusUnauthorized},
		{name: "websocket", method: http.MethodGet, path: "/ws", header: map[string]string{"Upgrade": "websocket", "Connection": "Upgrade"}, wantStatus: http.StatusUnauthorized},
		{name: "websocket topology", method: http.MethodGet, path: "/ws/topology", wantStatus: http.StatusUnauthorized},
		{name: "upgrade on another path", method: http.MethodGet, path: "/", header: map[string]string{"Upgrade": "websocket"}, wantStatus: http.StatusUnauthorized},
		{name: "ui root", method: http.MethodGet, path: "/", wantStatus: http.StatusFound, wantLocation: "/login"},
		{name: "ui deep link", method: http.MethodGet, path: "/assets/app.js?v=1", wantStatus: http.StatusFound, wantLocation: "/login?next=%2Fassets%2Fapp.js%3Fv%3D1"},
		{name: "ui head", method: http.MethodHead, path: "/", wantStatus: http.StatusFound, wantLocation: "/login"},
		{name: "ui post", method: http.MethodPost, path: "/", wantStatus: http.StatusUnauthorized},
		{name: "healthz lookalike", method: http.MethodGet, path: "/healthz/../api/topology", wantStatus: http.StatusFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authenticator, _ := newTestAuthenticator(t, Options{ProtectMetrics: tt.protectMetrics})
			request := httptest.NewRequest(tt.method, tt.path, nil)
			for key, value := range tt.header {
				request.Header.Set(key, value)
			}
			recorder := httptest.NewRecorder()
			testHandler(authenticator).ServeHTTP(recorder, request)
			response := recorder.Result()

			if response.StatusCode != tt.wantStatus {
				t.Fatalf("expected %d, got %d", tt.wantStatus, response.StatusCode)
			}
			if tt.wantLocation != "" && response.Header.Get("Location") != tt.wantLocation {
				t.Fatalf("expected redirect to %q, got %q", tt.wantLocation, response.Header.Get("Location"))
			}
			if response.StatusCode == http.StatusUnauthorized {
				if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
					t.Fatalf("expected JSON 401, got %q", contentType)
				}
				var payload struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal([]byte(readBody(t, response)), &payload); err != nil || payload.Error == "" {
					t.Fatalf("expected JSON error body, got %v", err)
				}
			}
		})
	}
}

func TestMiddlewareRejectsCookieFromAnotherDeployment(t *testing.T) {
	authenticator, _ := newTestAuthenticator(t, Options{})
	other := testCredentials
	other.SessionKey = strings.Repeat("o", MinSessionKeyLength)
	otherAuthenticator, err := New(other, Options{Now: newFakeClock().Now})
	if err != nil {
		t.Fatalf("create authenticator: %v", err)
	}
	cookie := sessionCookieFrom(t, postLogin(t, testHandler(otherAuthenticator), validLogin(), nil))

	if response := get(testHandler(authenticator), "/api/topology", cookie); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected foreign cookie to be rejected, got %d", response.StatusCode)
	}
}

func TestLogoutClearsSessionCookie(t *testing.T) {
	authenticator, _ := newTestAuthenticator(t, Options{})
	handler := testHandler(authenticator)

	request := httptest.NewRequest(http.MethodPost, LogoutPath, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	response := recorder.Result()

	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != LoginPath {
		t.Fatalf("expected redirect to login, got %d %q", response.StatusCode, response.Header.Get("Location"))
	}
	cookie := sessionCookieFrom(t, response)
	if cookie.Value != "" || cookie.MaxAge >= 0 || !cookie.HttpOnly || cookie.Path != "/" {
		t.Fatalf("expected an expired, empty session cookie, got %+v", cookie)
	}

	if response := get(handler, LogoutPath, nil); response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected GET /logout to be rejected, got %d", response.StatusCode)
	}

	crossOrigin := httptest.NewRequest(http.MethodPost, LogoutPath, nil)
	crossOrigin.Host = "northscope.internal"
	crossOrigin.Header.Set("Origin", "https://evil.example")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, crossOrigin)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected cross-origin logout to be rejected, got %d", recorder.Code)
	}
}

func TestLoginThrottlesRepeatedFailuresPerClient(t *testing.T) {
	authenticator, clock := newTestAuthenticator(t, Options{})
	handler := testHandler(authenticator)
	wrong := url.Values{"username": {"admin"}, "password": {"wrong"}}

	for attempt := 1; attempt <= loginBurst; attempt++ {
		if response := postLogin(t, handler, wrong, nil); response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", attempt, response.StatusCode)
		}
	}

	throttled := postLogin(t, handler, validLogin(), nil)
	body := readBody(t, throttled)
	if throttled.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 even with correct credentials, got %d", throttled.StatusCode)
	}
	if !strings.Contains(body, tooManyAttemptsMessage) {
		t.Fatalf("expected throttling message, got:\n%s", body)
	}
	if retryAfter := throttled.Header.Get("Retry-After"); retryAfter != strconv.Itoa(int(loginRefill.Seconds())) {
		t.Fatalf("unexpected Retry-After %q", retryAfter)
	}
	for _, cookie := range throttled.Cookies() {
		if cookie.Name == SessionCookieName {
			t.Fatal("throttled login must not set a session cookie")
		}
	}

	otherClient := postLogin(t, handler, validLogin(), func(r *http.Request) { r.RemoteAddr = "198.51.100.7:4000" })
	if otherClient.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected another client to be unaffected, got %d", otherClient.StatusCode)
	}

	clock.Advance(loginRefill)
	if response := postLogin(t, handler, validLogin(), nil); response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected a refilled attempt to succeed, got %d", response.StatusCode)
	}
}

func TestLoginThrottleBoundsTrackedClients(t *testing.T) {
	clock := newFakeClock()
	throttle := newLoginThrottle(1, time.Minute)
	for i := 0; i < maxThrottledClients+50; i++ {
		throttle.fail("client-"+strconv.Itoa(i), clock.Now())
	}
	// The limit plus the one shared overflow bucket.
	if len(throttle.clients) > maxThrottledClients+1 {
		t.Fatalf("tracked %d clients, limit is %d", len(throttle.clients), maxThrottledClients)
	}
	if ok, _ := throttle.allow("new-client", clock.Now()); ok {
		t.Fatal("expected clients beyond the limit to share the exhausted overflow bucket")
	}

	clock.Advance(time.Minute)
	if ok, _ := throttle.allow("new-client", clock.Now()); !ok {
		t.Fatal("expected refilled buckets to be pruned so new clients are tracked again")
	}
}

func TestSafeNext(t *testing.T) {
	for input, want := range map[string]string{
		"":                       "/",
		"/":                      "/",
		"/?namespace=demo":       "/?namespace=demo",
		"/assets/app.js":         "/assets/app.js",
		"relative":               "/",
		"//evil.example/path":    "/",
		"/\\evil.example":        "/",
		"https://evil.example/":  "/",
		"/login?next=%2F":        "/",
		"/logout":                "/",
		"/path\r\nSet-Cookie: x": "/",
	} {
		if got := safeNext(input); got != want {
			t.Fatalf("safeNext(%q) = %q, want %q", input, got, want)
		}
	}
}

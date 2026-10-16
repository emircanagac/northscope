package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func forwardedFrom(peer, forwardedFor string) func(*http.Request) {
	return func(r *http.Request) {
		r.RemoteAddr = peer + ":40000"
		if forwardedFor != "" {
			r.Header.Set("X-Forwarded-For", forwardedFor)
		}
	}
}

func TestLoginClientUsesProxyAppendedAddressOnlyFromTrustedPeers(t *testing.T) {
	tests := []struct {
		name          string
		peer          string
		forwardedFor  string
		realIP        string
		expectClient  string
		expectProxied bool
	}{
		{name: "in-cluster ingress controller", peer: "10.42.0.7", forwardedFor: "203.0.113.9", expectClient: "203.0.113.9", expectProxied: true},
		{name: "forged entries before the proxy's own", peer: "10.42.0.7", forwardedFor: "198.51.100.1, 203.0.113.9", expectClient: "203.0.113.9", expectProxied: true},
		{name: "X-Real-IP fallback", peer: "172.16.3.4", realIP: "203.0.113.20", expectClient: "203.0.113.20", expectProxied: true},
		{name: "untrusted peer ignores forwarded headers", peer: "203.0.113.50", forwardedFor: "198.51.100.1", expectClient: "203.0.113.50"},
		{name: "invalid forwarded value", peer: "10.42.0.7", forwardedFor: "not-an-ip", expectClient: "10.42.0.7"},
		{name: "no forwarded header", peer: "10.42.0.7", expectClient: "10.42.0.7"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, LoginPath, nil)
			request.RemoteAddr = test.peer + ":40000"
			if test.forwardedFor != "" {
				request.Header.Set("X-Forwarded-For", test.forwardedFor)
			}
			if test.realIP != "" {
				request.Header.Set("X-Real-IP", test.realIP)
			}
			client, proxy := loginClient(request)
			if client != test.expectClient {
				t.Fatalf("expected client %q, got %q", test.expectClient, client)
			}
			if (proxy != "") != test.expectProxied {
				t.Fatalf("expected proxied=%v, got proxy %q", test.expectProxied, proxy)
			}
		})
	}
}

func TestLoginThrottleSeparatesClientsBehindOneIngressController(t *testing.T) {
	authenticator, _ := newTestAuthenticator(t, Options{})
	handler := testHandler(authenticator)
	wrong := url.Values{"username": {"admin"}, "password": {"wrong"}}

	for attempt := 0; attempt <= loginBurst; attempt++ {
		postLogin(t, handler, wrong, forwardedFrom("10.42.0.7", "203.0.113.9"))
	}
	if response := postLogin(t, handler, validLogin(), forwardedFrom("10.42.0.7", "203.0.113.9")); response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected the failing client to be throttled, got %d", response.StatusCode)
	}
	if response := postLogin(t, handler, validLogin(), forwardedFrom("10.42.0.7", "203.0.113.10")); response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected another client behind the same controller to sign in, got %d", response.StatusCode)
	}
}

func TestLoginThrottleCapsForgedForwardedAddressesPerProxy(t *testing.T) {
	authenticator, clock := newTestAuthenticator(t, Options{})
	handler := testHandler(authenticator)
	wrong := url.Values{"username": {"admin"}, "password": {"wrong"}}

	for attempt := 0; attempt < proxyLoginBurst; attempt++ {
		forged := "198.51.100." + strconv.Itoa(attempt+1)
		if response := postLogin(t, handler, wrong, forwardedFrom("10.42.0.7", forged)); response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401 within the proxy budget, got %d", attempt+1, response.StatusCode)
		}
	}
	response := postLogin(t, handler, wrong, forwardedFrom("10.42.0.7", "198.51.100.200"))
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected forged addresses to hit the per-proxy limit, got %d", response.StatusCode)
	}
	if retryAfter := response.Header.Get("Retry-After"); retryAfter != strconv.Itoa(int(proxyLoginRefill.Seconds())) {
		t.Fatalf("expected Retry-After %v, got %q", proxyLoginRefill.Seconds(), retryAfter)
	}

	clock.Advance(proxyLoginRefill)
	if response := postLogin(t, handler, validLogin(), forwardedFrom("10.42.0.7", "203.0.113.30")); response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected sign-in after the proxy budget refills, got %d", response.StatusCode)
	}
}

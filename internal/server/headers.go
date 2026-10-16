package server

import "net/http"

// contentSecurityPolicy only allows same-origin resources. The embedded UI
// bundle loads its scripts, styles, and WebSocket from NorthScope itself.
const contentSecurityPolicy = "default-src 'self'; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'"

// securityHeaders adds browser hardening headers to every response. Handlers
// may replace the Content-Security-Policy, as the login page does.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Content-Security-Policy", contentSecurityPolicy)
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

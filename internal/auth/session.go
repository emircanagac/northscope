package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

const sessionVersion = "v1"

var (
	errMalformedSession = errors.New("malformed session cookie")
	errInvalidSignature = errors.New("invalid session signature")
	errExpiredSession   = errors.New("session expired")
)

// Session is a verified sign-in.
type Session struct {
	Username  string
	ExpiresAt time.Time
}

type sessionContextKey struct{}

// SessionFromContext returns the verified session that Middleware attached to
// the request context.
func SessionFromContext(ctx context.Context) (Session, bool) {
	session, ok := ctx.Value(sessionContextKey{}).(Session)
	return session, ok
}

func withSession(ctx context.Context, session Session) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, session)
}

// sessionSigner issues and verifies stateless session tokens of the form
// v1.<expires-unix>.<nonce>.<signature>. The signing key is derived from the
// session key and the credentials, so rotating the password or the session key
// invalidates every existing session.
type sessionSigner struct {
	key []byte
	ttl time.Duration
}

func newSessionSigner(creds Credentials, ttl time.Duration) sessionSigner {
	mac := hmac.New(sha256.New, []byte(creds.SessionKey))
	mac.Write([]byte("northscope-session-" + sessionVersion))
	mac.Write([]byte{0})
	mac.Write([]byte(creds.Username))
	mac.Write([]byte{0})
	mac.Write([]byte(creds.Password))
	return sessionSigner{key: mac.Sum(nil), ttl: ttl}
}

func (s sessionSigner) issue(now time.Time) (string, time.Time, error) {
	expiresAt := now.Add(s.ttl).Truncate(time.Second)
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", time.Time{}, err
	}
	payload := sessionVersion + "." + strconv.FormatInt(expiresAt.Unix(), 10) + "." + base64.RawURLEncoding.EncodeToString(nonce)
	return payload + "." + base64.RawURLEncoding.EncodeToString(s.sign(payload)), expiresAt, nil
}

func (s sessionSigner) verify(token string, now time.Time) (time.Time, error) {
	separator := strings.LastIndexByte(token, '.')
	if separator < 0 {
		return time.Time{}, errMalformedSession
	}
	payload := token[:separator]
	signature, err := base64.RawURLEncoding.DecodeString(token[separator+1:])
	if err != nil {
		return time.Time{}, errMalformedSession
	}
	if !hmac.Equal(signature, s.sign(payload)) {
		return time.Time{}, errInvalidSignature
	}

	parts := strings.Split(payload, ".")
	if len(parts) != 3 || parts[0] != sessionVersion {
		return time.Time{}, errMalformedSession
	}
	expiresUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, errMalformedSession
	}
	expiresAt := time.Unix(expiresUnix, 0)
	if !now.Before(expiresAt) {
		return time.Time{}, errExpiredSession
	}
	// A validly signed token never outlives the configured lifetime; reject
	// anything that claims to, for example after the TTL was shortened.
	if expiresAt.Sub(now) > s.ttl+time.Second {
		return time.Time{}, errExpiredSession
	}
	return expiresAt, nil
}

func (s sessionSigner) sign(payload string) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

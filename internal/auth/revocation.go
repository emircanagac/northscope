package auth

import (
	"sync"
	"time"
)

// maxRevokedSessions bounds the memory that remembered sign-outs use. Only a
// signed-in user can add entries, and each one is dropped when its session
// would have expired anyway.
const maxRevokedSessions = 10000

// sessionRevocations remembers signed-out sessions until they expire, so a
// copied session cookie stops working once its owner signs out. Sessions are
// otherwise stateless, so the list lives in process memory: other replicas
// and a restarted process do not know about earlier sign-outs.
type sessionRevocations struct {
	mu       sync.Mutex
	sessions map[string]time.Time
}

func newSessionRevocations() *sessionRevocations {
	return &sessionRevocations{sessions: map[string]time.Time{}}
}

// revoke records a signed-out session until expiresAt. It reports false when
// the list is full of sessions that have not expired yet.
func (r *sessionRevocations) revoke(id string, expiresAt, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for revoked, expiry := range r.sessions {
		if !now.Before(expiry) {
			delete(r.sessions, revoked)
		}
	}
	if _, ok := r.sessions[id]; !ok && len(r.sessions) >= maxRevokedSessions {
		return false
	}
	r.sessions[id] = expiresAt
	return true
}

// revoked reports whether the session was signed out.
func (r *sessionRevocations) revoked(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.sessions[id]
	return ok
}

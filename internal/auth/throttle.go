package auth

import (
	"math"
	"sync"
	"time"
)

const (
	// loginBurst failed sign-ins are allowed per client before throttling.
	loginBurst = 5
	// loginRefill is how long a client waits for each further attempt.
	loginRefill = 30 * time.Second
	// proxyLoginBurst and proxyLoginRefill bound all failed sign-ins forwarded
	// by one trusted proxy, so forged X-Forwarded-For values cannot bypass the
	// per-client limit, while one client's typos no longer block everyone.
	proxyLoginBurst  = 50
	proxyLoginRefill = 2 * time.Second
	// maxThrottledClients bounds memory; beyond it new clients share one bucket.
	maxThrottledClients = 10000
	overflowClientKey   = "*"
)

// loginThrottle is a per-client token bucket that only failed sign-ins drain.
// A client with an empty bucket is rejected before its credentials are checked.
type loginThrottle struct {
	mu      sync.Mutex
	burst   float64
	refill  time.Duration
	clients map[string]*loginBucket
}

type loginBucket struct {
	tokens  float64
	updated time.Time
}

func newLoginThrottle(burst int, refill time.Duration) *loginThrottle {
	return &loginThrottle{
		burst:   float64(burst),
		refill:  refill,
		clients: map[string]*loginBucket{},
	}
}

// allow reports whether the client may attempt a sign-in now and, if not, how
// long it has to wait.
func (t *loginThrottle) allow(client string, now time.Time) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	bucket := t.bucket(client, now, false)
	if bucket == nil || bucket.tokens >= 1 {
		return true, 0
	}
	wait := time.Duration((1 - bucket.tokens) * float64(t.refill))
	return false, wait
}

// fail records a failed sign-in for the client.
func (t *loginThrottle) fail(client string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	bucket := t.bucket(client, now, true)
	bucket.tokens = math.Max(0, bucket.tokens-1)
}

// bucket returns the client's bucket refilled to now. Clients without failures
// have no bucket unless create is set.
func (t *loginThrottle) bucket(client string, now time.Time, create bool) *loginBucket {
	if _, ok := t.clients[client]; !ok && len(t.clients) >= maxThrottledClients {
		t.prune(now)
		if len(t.clients) >= maxThrottledClients {
			client = overflowClientKey
		}
	}

	bucket, ok := t.clients[client]
	if !ok {
		if !create {
			return nil
		}
		bucket = &loginBucket{tokens: t.burst, updated: now}
		t.clients[client] = bucket
	}
	if elapsed := now.Sub(bucket.updated); elapsed > 0 {
		bucket.tokens = math.Min(t.burst, bucket.tokens+float64(elapsed)/float64(t.refill))
		bucket.updated = now
	}
	return bucket
}

// prune drops buckets that have refilled completely.
func (t *loginThrottle) prune(now time.Time) {
	for client, bucket := range t.clients {
		if bucket.tokens+float64(now.Sub(bucket.updated))/float64(t.refill) >= t.burst {
			delete(t.clients, client)
		}
	}
}

package gateway

import (
	"sync"
	"time"
)

// healthCache memoises the most recent health response for 1 second. The 1s
// window is short enough to be invisible to legitimate K8s probers (default
// liveness/readiness intervals are 10s) and long enough to flatten the
// "scanner sees /health" burst pattern. Only the status code + body are
// cached; we do not cache headers because the response is identical bytes
// either way.
type healthCache struct {
	mu        sync.Mutex
	expiresAt time.Time
	status    int
	body      []byte
}

func (c *healthCache) get(now time.Time) (status int, body []byte, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Before(c.expiresAt) {
		return c.status, c.body, true
	}
	return 0, nil, false
}

func (c *healthCache) set(status int, body []byte, ttl time.Duration, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]byte, len(body))
	copy(cp, body)
	c.status = status
	c.body = cp
	c.expiresAt = now.Add(ttl)
}

package gateway

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// principalLimiters keeps a `*rate.Limiter` per principal key. The key is the
// caller's identity hash plus their remote IP; see auth.go for the derivation.
// The map is bounded by `cap` entries and the least-recently-seen entry is
// evicted when full. Background sweeper drops idle entries on a 5-minute
// window. The limiter rate and burst come straight from gateway config.
type principalLimiters struct {
	mu    sync.Mutex
	m     map[string]*limiterEntry
	rate  rate.Limit
	burst int
	cap   int
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newPrincipalLimiters(r rate.Limit, burst, capacity int) *principalLimiters {
	if capacity <= 0 {
		capacity = 10000
	}
	return &principalLimiters{
		m:     make(map[string]*limiterEntry),
		rate:  r,
		burst: burst,
		cap:   capacity,
	}
}

func (p *principalLimiters) get(key string) *rate.Limiter {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.m[key]; ok {
		e.lastSeen = now
		return e.limiter
	}
	if len(p.m) >= p.cap {
		p.evictOldestLocked()
	}
	e := &limiterEntry{limiter: rate.NewLimiter(p.rate, p.burst), lastSeen: now}
	p.m[key] = e
	return e.limiter
}

// evictOldestLocked drops the entry with the oldest lastSeen. Caller holds
// p.mu. O(n) over the map — acceptable because eviction only fires at the
// configured cap (10k by default).
func (p *principalLimiters) evictOldestLocked() {
	var oldestKey string
	var oldestSeen time.Time
	first := true
	for k, e := range p.m {
		if first || e.lastSeen.Before(oldestSeen) {
			oldestKey, oldestSeen = k, e.lastSeen
			first = false
		}
	}
	if oldestKey != "" {
		delete(p.m, oldestKey)
	}
}

// sweep removes entries last seen before `cutoff`. Returns the number removed.
func (p *principalLimiters) sweep(cutoff time.Time) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	removed := 0
	for k, e := range p.m {
		if e.lastSeen.Before(cutoff) {
			delete(p.m, k)
			removed++
		}
	}
	return removed
}

func (p *principalLimiters) len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.m)
}

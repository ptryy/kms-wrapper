package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	apptypes "github.com/ryan-truong/kms-wrapper/pkg/types"
)

type chainsCacheEntry struct {
	chains []apptypes.Chain
	at     time.Time
}

type chainsCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]chainsCacheEntry
}

func newChainsCache(ttl time.Duration) *chainsCache {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &chainsCache{
		ttl:     ttl,
		entries: map[string]chainsCacheEntry{},
	}
}

func (c *chainsCache) get(path string, now time.Time) ([]apptypes.Chain, bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[path]
	if !ok {
		return nil, false, false
	}
	out := append([]apptypes.Chain(nil), entry.chains...)
	return out, now.Sub(entry.at) <= c.ttl, true
}

func (c *chainsCache) set(path string, chains []apptypes.Chain, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[path] = chainsCacheEntry{
		chains: append([]apptypes.Chain(nil), chains...),
		at:     now,
	}
}

func (c *chainsCache) invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, path)
}

func canonicalizeChains(raw []string) ([]apptypes.Chain, error) {
	if len(raw) == 0 {
		return []apptypes.Chain{}, nil
	}
	chains, err := apptypes.ParseChains(raw)
	if err != nil {
		return nil, err
	}
	return chains, nil
}

func (s *Server) authorizeChain(ctx context.Context, path string, attempted apptypes.Chain) ([]apptypes.Chain, int, error) {
	now := time.Now()
	if s.chains != nil {
		if cached, fresh, ok := s.chains.get(path, now); ok && fresh {
			if apptypes.ChainsContain(cached, attempted) {
				return cached, 0, nil
			}
			return s.authorizeChainFetch(ctx, path, attempted)
		}
	}
	return s.authorizeChainFetch(ctx, path, attempted)
}

func (s *Server) authorizeChainFetch(ctx context.Context, path string, attempted apptypes.Chain) ([]apptypes.Chain, int, error) {
	rawChains, err := s.keys.GetKeyChains(ctx, path)
	if err != nil {
		if errors.Is(err, apptypes.ErrNotFound) {
			return nil, http.StatusNotFound, err
		}
		if errors.Is(err, apptypes.ErrPermission) {
			return nil, http.StatusForbidden, err
		}
		return nil, http.StatusServiceUnavailable, err
	}
	chains, err := canonicalizeChains(rawChains)
	if err != nil {
		return nil, http.StatusServiceUnavailable, err
	}
	if s.chains != nil {
		s.chains.set(path, chains, time.Now())
	}
	if apptypes.ChainsContain(chains, attempted) {
		return chains, 0, nil
	}
	kmsChainAuthzDenialsTotal.WithLabelValues(string(attempted)).Inc()
	return chains, http.StatusForbidden, fmt.Errorf("key %s not authorized for %s signing (allowed chains: %v)", path, attempted, chains)
}

func (s *Server) writeChainAuthzResult(w http.ResponseWriter, r *http.Request, keyPath string, attempted apptypes.Chain, allowed []apptypes.Chain, status int, err error) bool {
	switch status {
	case 0:
		return true
	case http.StatusForbidden:
		if errors.Is(err, apptypes.ErrPermission) {
			slog.WarnContext(r.Context(), "chain authorization lookup permission denied",
				"key_path", keyPath,
				"attempted_chain", attempted,
			)
			writeError(w, status, "permission denied")
			return false
		}
		slog.WarnContext(r.Context(), "chain authorization denied",
			"key_path", keyPath,
			"attempted_chain", attempted,
			"allowed_chains", allowed,
		)
		writeError(w, status, err.Error())
	case http.StatusServiceUnavailable:
		slog.ErrorContext(r.Context(), "chain authorization lookup unavailable",
			"error", err,
			"key_path", keyPath,
			"attempted_chain", attempted,
		)
		writeError(w, status, "chain authorization unavailable")
	case http.StatusNotFound:
		s.writeVaultErr(w, r, err, keyPath, "GetKeyChains")
	default:
		slog.ErrorContext(r.Context(), "chain authorization failed",
			"error", err,
			"key_path", keyPath,
			"attempted_chain", attempted,
		)
		writeError(w, http.StatusInternalServerError, "signing failed")
	}
	return false
}

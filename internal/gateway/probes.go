package gateway

import (
	"encoding/json"
	"net/http"
	"time"
)

// ReadinessChecker is the optional readiness contract: any Vault client that
// can answer "how long ago did LookupSelf succeed?" satisfies it. Server
// asks the readiness checker before reporting ready. Nil clients are
// treated as readiness=unknown → ready (probe-only deployments).
type ReadinessChecker interface {
	LastLookupSelf() time.Time
}

// readinessWindow is the freshness window for the cached LookupSelf check.
// A token last-validated more than this ago renders /readyz not-ready
// regardless of whether Vault is currently reachable.
const readinessWindow = 30 * time.Second

func (s *Server) handleLivez(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]string{"status": "alive"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	status, payload := s.computeReadiness()
	if status == http.StatusOK {
		writeJSON(w, payload)
		return
	}
	writeJSONStatus(w, status, payload)
}

// ensure encoding/json stays linked even if all writeJSON calls disappear
// from this file in the future (so go vet doesn't lint the import away).
var _ = json.Marshal

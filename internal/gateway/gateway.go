package gateway

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	httpSwagger "github.com/swaggo/http-swagger/v2"
	swagv1 "github.com/swaggo/swag"
	"golang.org/x/time/rate"

	"github.com/ryan-truong/kms-wrapper/internal/config"
	"github.com/ryan-truong/kms-wrapper/internal/keyinfo"
	cosmossigner "github.com/ryan-truong/kms-wrapper/internal/signer/cosmos"
	"github.com/ryan-truong/kms-wrapper/internal/signer/evm"
	"github.com/ryan-truong/kms-wrapper/internal/vault"
	apptypes "github.com/ryan-truong/kms-wrapper/pkg/types"
)

type HealthChecker interface{ Health() error }

type KeyStore interface {
	CreateKey(ctx context.Context, path string, chains []string) error
	UpdateKeyChains(ctx context.Context, path string, addChains []string) ([]string, error)
	GetPublicKey(ctx context.Context, path string) ([]byte, error)
	GetKeyChains(ctx context.Context, path string) ([]string, error)
	ListKeys(ctx context.Context, prefix string) ([]string, error)
}

type EVMSigner interface {
	SignRawTx(ctx context.Context, keyPath string, chainID *big.Int, rawTx []byte) ([]byte, error)
	SignPersonalMessage(ctx context.Context, keyPath string, msg []byte) ([]byte, error)
	SignEIP712Digest(ctx context.Context, keyPath string, digest []byte) ([]byte, error)
}

type CosmosSigner interface {
	SignDirect(ctx context.Context, keyPath string, signDocBytes []byte) ([]byte, []byte, error)
	SignAmino(ctx context.Context, keyPath string, stdSignDocJSON []byte) ([]byte, []byte, error)
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// methodNotAllowedRewriter intercepts a 405 emitted by http.ServeMux and
// replaces the plain-text body with the gateway's standard JSON error shape.
type methodNotAllowedRewriter struct {
	http.ResponseWriter
	swallowBody bool
}

func (w *methodNotAllowedRewriter) WriteHeader(status int) {
	if status == http.StatusMethodNotAllowed {
		h := w.ResponseWriter.Header()
		// Preserve the `Allow` header set by http.ServeMux. Per RFC 7231
		// §6.5.5 a 405 response MUST include `Allow`; the rewriter only
		// replaces the body, never the headers that classify the failure.
		allow := h.Get("Allow")
		h.Set("Content-Type", "application/json")
		h.Del("Content-Length")
		if allow != "" {
			h.Set("Allow", allow)
		}
		w.ResponseWriter.WriteHeader(status)
		_, _ = w.ResponseWriter.Write([]byte("{\"error\":\"method not allowed\"}\n"))
		w.swallowBody = true
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *methodNotAllowedRewriter) Write(b []byte) (int, error) {
	if w.swallowBody {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

func json405Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&methodNotAllowedRewriter{ResponseWriter: w}, r)
	})
}

type Server struct {
	cfg            config.Config
	vault          HealthChecker
	keys           KeyStore
	evm            EVMSigner
	cosmos         CosmosSigner
	server         *http.Server
	principals     *principalLimiters
	healthLimiters *principalLimiters
	healthResp     healthCache
	chains         *chainsCache
	serverNonce    []byte
	trustedProxies []*net.IPNet
}

// New constructs a Server. Errors here surface configuration mistakes (bad
// CIDRs, RNG failure) up to the caller so startup fails fast rather than
// silently disabling a security gate.
func New(cfg config.Config, vault HealthChecker, keys KeyStore, evmSigner EVMSigner, cosmosSigner CosmosSigner) *Server {
	s, err := NewOrFail(cfg, vault, keys, evmSigner, cosmosSigner)
	if err != nil {
		panic(err)
	}
	return s
}

// NewOrFail is the error-returning constructor used by production callers
// that want to surface configuration errors (bad trusted-proxy CIDRs, RNG
// failure) instead of panicking. New() wraps this for tests and existing
// callsites that already assume the constructor cannot fail.
func NewOrFail(cfg config.Config, vault HealthChecker, keys KeyStore, evmSigner EVMSigner, cosmosSigner CosmosSigner) (*Server, error) {
	if cfg.Gateway.Addr == "" {
		cfg.Gateway.Addr = "127.0.0.1:8080"
	}
	rl := cfg.Gateway.RateLimit
	if rl <= 0 {
		rl = 100
	}
	burst := cfg.Gateway.RateBurst
	if burst <= 0 {
		burst = 20
	}
	healthRate := cfg.Gateway.HealthRateLimit
	if healthRate <= 0 {
		healthRate = 10
	}
	healthBurst := cfg.Gateway.HealthRateBurst
	if healthBurst <= 0 {
		healthBurst = 5
	}
	nonce := make([]byte, 32)
	if _, err := cryptorand.Read(nonce); err != nil {
		return nil, err
	}
	trusted, err := parseTrustedProxies(cfg.Gateway.TrustedProxies)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:            cfg,
		vault:          vault,
		keys:           keys,
		evm:            evmSigner,
		cosmos:         cosmosSigner,
		principals:     newPrincipalLimiters(rate.Limit(rl), burst, 10000),
		healthLimiters: newPrincipalLimiters(rate.Limit(healthRate), healthBurst, 10000),
		serverNonce:    nonce,
		chains:         newChainsCache(cfg.Gateway.ChainsCacheTTL),
		trustedProxies: trusted,
	}
	s.server = &http.Server{
		Addr:              cfg.Gateway.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return s, nil
}

// StartLimiterSweeper launches a background goroutine that removes idle
// limiter entries every minute. It exits on ctx.Done().
func (s *Server) StartLimiterSweeper(ctx context.Context) {
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cutoff := time.Now().Add(-5 * time.Minute)
				s.principals.sweep(cutoff)
				s.healthLimiters.sweep(cutoff)
			}
		}
	}()
}

func (s *Server) ListenAndServe() error {
	errCh := make(chan error, 1)
	go func() {
		var err error
		if s.cfg.Gateway.TLSCertFile != "" && s.cfg.Gateway.TLSKeyFile != "" {
			err = s.server.ListenAndServeTLS(s.cfg.Gateway.TLSCertFile, s.cfg.Gateway.TLSKeyFile)
		} else {
			err = s.server.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return s.server.Shutdown(ctx)
	}
}

func (s *Server) Handler() http.Handler { return s.routes() }

// routesList enumerates the canonical (un-prefixed) routes. Each entry is
// registered twice: once at its bare path with a Deprecation/Sunset wrapper
// (for backwards-compat) and once at the `/v1` prefix that the OpenAPI spec
// advertises as primary. The bare paths are scheduled to be removed in the
// next minor-version cycle per RFC 8594.
type routeEntry struct {
	method  string
	pattern string
	handler http.Handler
}

func (s *Server) appRoutes() []routeEntry {
	return []routeEntry{
		{http.MethodGet, "/health", s.healthRateLimit(http.HandlerFunc(s.health))},
		{http.MethodPost, "/sign/evm", s.rateLimit(s.auth(http.HandlerFunc(s.signEVM)))},
		{http.MethodPost, "/sign/cosmos", s.rateLimit(s.auth(http.HandlerFunc(s.signCosmos)))},
		{http.MethodPost, "/keys", s.rateLimit(s.auth(http.HandlerFunc(s.createKey)))},
		{http.MethodPatch, "/keys/{path...}", s.rateLimit(s.auth(http.HandlerFunc(s.updateKeyChains)))},
		{http.MethodGet, "/keys/info", s.rateLimit(s.auth(http.HandlerFunc(s.showKey)))},
		{http.MethodGet, "/keys", s.rateLimit(s.auth(http.HandlerFunc(s.listKeys)))},
	}
}

// sunsetDate is the RFC 8594 Sunset header value advertised on the bare
// route family. Set once per process start, at least 90 days in the future.
var sunsetDate = time.Now().AddDate(0, 0, 120).UTC().Format(http.TimeFormat)

// withDeprecation tags responses with the bare-path Deprecation + Sunset
// headers. Mounted only on the bare-form copy of each route; the `/v1/`
// variant returns unadorned responses.
func withDeprecation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Sunset", sunsetDate)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	for _, e := range s.appRoutes() {
		mux.Handle(e.method+" "+e.pattern, withDeprecation(e.handler))
		mux.Handle(e.method+" /v1"+e.pattern, e.handler)
	}
	// Probe + observability endpoints. Unauthenticated and IP-rate-limited
	// via the slow-path limiter. Mounted at bare paths (not /v1/) per
	// convention; `/health` keeps backward-compat with Deprecation+Sunset.
	mux.Handle("GET /livez", s.healthRateLimit(http.HandlerFunc(s.handleLivez)))
	mux.Handle("GET /readyz", s.healthRateLimit(http.HandlerFunc(s.handleReadyz)))
	mux.Handle("GET /metrics", s.healthRateLimit(metricsHandler()))
	if s.cfg.Gateway.SwaggerEnabled {
		swaggerUI := httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json"))
		// Rewrite "/swagger/" → "/swagger/index.html" so http-swagger serves the
		// UI body directly with HTTP 200 instead of a cacheable 301 redirect.
		// http-swagger matches on r.RequestURI, so rewrite that too.
		swaggerRoot := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/swagger/" {
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/swagger/index.html"
				r2.RequestURI = "/swagger/index.html"
				swaggerUI.ServeHTTP(w, r2)
				return
			}
			swaggerUI.ServeHTTP(w, r)
		}))
		var swaggerDoc http.Handler = http.HandlerFunc(s.serveSwaggerDoc)
		if s.cfg.Gateway.SwaggerAuth {
			swaggerRoot = s.auth(swaggerRoot)
			swaggerDoc = s.auth(swaggerDoc)
		}
		mux.Handle("GET /swagger/doc.json", swaggerDoc)
		mux.Handle("GET /swagger/", swaggerRoot)
	}
	// Middleware order: requestID → recoverPanic → requestLogger → 405-rewriter → mux.
	// requestID must be outermost so the recovered request still has the ID
	// in its context; recoverPanic sits immediately inside so it captures
	// the request ID for the 500 envelope and the panic log line.
	return requestID(recoverPanic(s.requestLogger(s.instrumentRoutes(json405Handler(mux)))))
}

// instrumentRoutes emits kms_http_requests_total + duration histograms.
// `path` uses the matched route pattern when possible (kept bounded), but
// since http.ServeMux does not expose the matched pattern post-dispatch we
// take r.URL.Path as a best-effort approximation. Cardinality is bounded
// by the finite route set.
func (s *Server) instrumentRoutes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		path := r.URL.Path
		method := r.Method
		status := http.StatusText(sw.status)
		if status == "" {
			status = "unknown"
		}
		kmsHTTPRequestsTotal.WithLabelValues(path, method, intToStatusLabel(sw.status)).Inc()
		kmsHTTPRequestDuration.WithLabelValues(path, method).Observe(time.Since(start).Seconds())
	})
}

func intToStatusLabel(s int) string {
	// 200 → "200", 404 → "404". Small allocation but bounded by status set.
	switch {
	case s >= 200 && s < 300:
		return fmt.Sprintf("%d", s)
	case s == 0:
		return "0"
	default:
		return fmt.Sprintf("%d", s)
	}
}

func (s *Server) serveSwaggerDoc(w http.ResponseWriter, r *http.Request) {
	doc, err := swagv1.ReadDoc("swagger")
	if err != nil {
		slog.ErrorContext(r.Context(), "load swagger doc failed", "error", err)
		writeError(w, http.StatusInternalServerError, "swagger doc unavailable")
		return
	}
	normalized, err := normalizeSwaggerDocServers(doc, s.resolveOrigin(r))
	if err != nil {
		slog.ErrorContext(r.Context(), "normalize swagger doc failed", "error", err)
		writeError(w, http.StatusInternalServerError, "swagger doc unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(normalized)
}

func normalizeSwaggerDocServers(doc, origin string) ([]byte, error) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(doc), &spec); err != nil {
		return nil, err
	}
	if origin != "" {
		spec["servers"] = []map[string]string{
			{"url": strings.TrimRight(origin, "/") + "/"},
		}
	}
	return json.Marshal(spec)
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.InfoContext(r.Context(), "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	})
}

// rateLimit applies the per-principal limiter to authenticated routes.
// The principal key is HMAC(serverNonce, bearer)||ip — see authHMAC.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := s.principalKey(r)
		if !s.principals.get(key).Allow() {
			s.onRateLimitRejected(r)
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// healthRateLimit is the slow-path limiter for unauthenticated probe/scrape
// endpoints (/health, /metrics). It keys on remote IP only — no bearer in
// scope. The rate/burst defaults are conservative enough for K8s probes.
func (s *Server) healthRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "ip|" + ipFromRemoteAddr(r.RemoteAddr)
		if !s.healthLimiters.get(key).Allow() {
			s.onRateLimitRejected(r)
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// onRateLimitRejected emits a single info-level log line (info, not warn —
// rate-limit rejection is expected behaviour under load) and increments the
// kms_rate_limit_rejections_total counter. `path` is r.URL.Path; the route
// set is finite so cardinality stays bounded.
func (s *Server) onRateLimitRejected(r *http.Request) {
	slog.InfoContext(r.Context(), "rate limit exceeded",
		"path", r.URL.Path,
		"reason", "rate_limited",
	)
	kmsRateLimitRejectionsTotal.WithLabelValues(r.URL.Path).Inc()
}

// health godoc
// @Summary Gateway health status (alias of /readyz; deprecated)
// @Tags health
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Security
// @Router /v1/health [get]
//
// health is the deprecated alias of /readyz that returns the same body shape
// but is cached for 1 second to absorb micro-bursts (legacy K8s probe
// pattern). Mounted at the bare `/health` path via the dual-mount loop, so
// the `withDeprecation` middleware adds the Deprecation and Sunset headers.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	if status, body, ok := s.healthResp.get(now); ok {
		w.Header().Set("Content-Type", "application/json")
		if status != http.StatusOK {
			w.WriteHeader(status)
		}
		_, _ = w.Write(body)
		return
	}
	status, payload := s.computeReadiness()
	body, _ := json.Marshal(payload)
	body = append(body, '\n')
	s.healthResp.set(status, body, time.Second, now)
	w.Header().Set("Content-Type", "application/json")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	_, _ = w.Write(body)
}

// computeReadiness returns the canonical (/readyz) status code + payload
// pair, shared by /readyz and the cached /health alias. Kept on Server so
// future Vault-state introspection can be added in one place.
func (s *Server) computeReadiness() (int, map[string]string) {
	if s.vault != nil {
		if err := s.vault.Health(); err != nil {
			return http.StatusServiceUnavailable, map[string]string{
				"status": "not_ready", "reason": "vault_unreachable",
			}
		}
	}
	if rc, ok := s.vault.(ReadinessChecker); ok {
		last := rc.LastLookupSelf()
		if last.IsZero() {
			return http.StatusServiceUnavailable, map[string]string{
				"status": "not_ready", "reason": "token_lookup_stale",
			}
		}
		if time.Since(last) > readinessWindow {
			return http.StatusServiceUnavailable, map[string]string{
				"status": "not_ready", "reason": "token_invalid",
			}
		}
	}
	return http.StatusOK, map[string]string{"status": "ready"}
}

// signEVM godoc
// @Summary Sign EVM payload
// @Tags signing
// @Accept json
// @Produce json
// @Param rawTx body apptypes.EVMSignRawTxRequest true "Raw-transaction payload"
// @Param personalMessage body apptypes.EVMSignPersonalMessageRequest true "Personal-message payload"
// @Param eip712 body apptypes.EVMSignEIP712Request true "EIP-712 digest payload"
// @Success 200 {object} apptypes.SignResponse
// @Failure 400 {object} apptypes.ErrorResponse
// @Failure 401 {object} apptypes.ErrorResponse
// @Failure 429 {object} apptypes.ErrorResponse
// @Failure 500 {object} apptypes.ErrorResponse
// @Security BearerAuth
// @Router /v1/sign/evm [post]
func (s *Server) signEVM(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req apptypes.EVMSignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.KeyPath == "" {
		writeError(w, http.StatusBadRequest, "key_path is required")
		return
	}
	// Validate before authorizeChain so a malformed key_path returns 400, not a
	// 503 from GetKeyChains' internal validation (consistent with createKey/showKey).
	if err := vault.ValidateKeyPath(req.KeyPath); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch req.Type {
	case "raw_tx":
		s.signEVMRawTx(w, r, &req)
	case "personal_message":
		s.signEVMPersonal(w, r, &req)
	case "eip712_digest":
		s.signEVMEIP712(w, r, &req)
	case "":
		writeError(w, http.StatusBadRequest, "type is required and must be one of raw_tx|personal_message|eip712_digest")
	default:
		writeError(w, http.StatusBadRequest, "type must be one of raw_tx|personal_message|eip712_digest")
	}
}

func (s *Server) signEVMRawTx(w http.ResponseWriter, r *http.Request, req *apptypes.EVMSignRequest) {
	defer func(start time.Time) { ObserveSigningDuration("evm", time.Since(start).Seconds()) }(time.Now())
	if req.RawTx == "" {
		writeError(w, http.StatusBadRequest, "raw_tx is required when type=raw_tx")
		return
	}
	if req.ChainID <= 0 {
		writeError(w, http.StatusBadRequest, "chain_id is required and must be positive")
		return
	}
	raw, err := decodeHex(req.RawTx)
	if err != nil {
		writeError(w, http.StatusBadRequest, "raw_tx must be hex")
		return
	}
	allowed, status, authzErr := s.authorizeChain(r.Context(), req.KeyPath, apptypes.ChainEVM)
	if !s.writeChainAuthzResult(w, r, req.KeyPath, apptypes.ChainEVM, allowed, status, authzErr) {
		return
	}
	out, err := s.evm.SignRawTx(r.Context(), req.KeyPath, big.NewInt(req.ChainID), raw)
	if err != nil {
		slog.ErrorContext(r.Context(), "EVM raw tx signing failed", "error", err, "key_path", req.KeyPath)
		writeError(w, http.StatusInternalServerError, "signing failed")
		return
	}
	var tx ethtypes.Transaction
	if err := tx.UnmarshalBinary(out); err != nil {
		slog.ErrorContext(r.Context(), "decode signed tx failed", "error", err)
		writeError(w, http.StatusInternalServerError, "decode signed tx: "+err.Error())
		return
	}
	v, rpart, spart := tx.RawSignatureValues()
	writeJSON(w, apptypes.SignResponse{
		SignedTx: "0x" + hex.EncodeToString(out),
		Parts:    &apptypes.SignatureParts{R: rpart.Text(16), S: spart.Text(16), V: v.Uint64()},
	})
}

func (s *Server) signEVMPersonal(w http.ResponseWriter, r *http.Request, req *apptypes.EVMSignRequest) {
	defer func(start time.Time) { ObserveSigningDuration("evm", time.Since(start).Seconds()) }(time.Now())
	if req.PersonalMessage == "" {
		writeError(w, http.StatusBadRequest, "personal_message is required when type=personal_message")
		return
	}
	msg, err := decodeHex(req.PersonalMessage)
	if err != nil {
		writeError(w, http.StatusBadRequest, "personal_message must be hex")
		return
	}
	allowed, status, authzErr := s.authorizeChain(r.Context(), req.KeyPath, apptypes.ChainEVM)
	if !s.writeChainAuthzResult(w, r, req.KeyPath, apptypes.ChainEVM, allowed, status, authzErr) {
		return
	}
	sig, err := s.evm.SignPersonalMessage(r.Context(), req.KeyPath, msg)
	if err != nil {
		slog.ErrorContext(r.Context(), "personal message signing failed", "error", err, "key_path", req.KeyPath)
		writeError(w, http.StatusInternalServerError, "signing failed")
		return
	}
	// eth_sign / personal_sign expects v=27/28
	writeJSON(w, apptypes.EVMSignPersonalResponse{Signature: "0x" + hex.EncodeToString(evm.NormalizeEthereumV(sig))})
}

func (s *Server) signEVMEIP712(w http.ResponseWriter, r *http.Request, req *apptypes.EVMSignRequest) {
	defer func(start time.Time) { ObserveSigningDuration("evm", time.Since(start).Seconds()) }(time.Now())
	if req.EIP712Digest == "" {
		writeError(w, http.StatusBadRequest, "eip712_digest is required when type=eip712_digest")
		return
	}
	digest, err := decodeHex(req.EIP712Digest)
	if err != nil {
		writeError(w, http.StatusBadRequest, "eip712_digest must be hex")
		return
	}
	if len(digest) != 32 {
		writeError(w, http.StatusBadRequest, "eip712_digest must be exactly 32 bytes")
		return
	}
	allowed, status, authzErr := s.authorizeChain(r.Context(), req.KeyPath, apptypes.ChainEVM)
	if !s.writeChainAuthzResult(w, r, req.KeyPath, apptypes.ChainEVM, allowed, status, authzErr) {
		return
	}
	sig, err := s.evm.SignEIP712Digest(r.Context(), req.KeyPath, digest)
	if err != nil {
		slog.ErrorContext(r.Context(), "EIP-712 signing failed", "error", err, "key_path", req.KeyPath)
		writeError(w, http.StatusInternalServerError, "signing failed")
		return
	}
	writeJSON(w, apptypes.EVMSignPersonalResponse{Signature: "0x" + hex.EncodeToString(sig)})
}

// signCosmos godoc
// @Summary Sign Cosmos payload
// @Tags signing
// @Accept json
// @Produce json
// @Param body body apptypes.CosmosSignRequest true "Cosmos sign payload"
// @Success 200 {object} apptypes.SignResponse
// @Failure 400 {object} apptypes.ErrorResponse
// @Failure 401 {object} apptypes.ErrorResponse
// @Failure 429 {object} apptypes.ErrorResponse
// @Failure 500 {object} apptypes.ErrorResponse
// @Security BearerAuth
// @Router /v1/sign/cosmos [post]
func (s *Server) signCosmos(w http.ResponseWriter, r *http.Request) {
	defer func(start time.Time) { ObserveSigningDuration("cosmos", time.Since(start).Seconds()) }(time.Now())
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req apptypes.CosmosSignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.KeyPath == "" {
		writeError(w, http.StatusBadRequest, "key_path is required")
		return
	}
	// Validate before authorizeChain so a malformed key_path returns 400, not a
	// 503 from GetKeyChains' internal validation (consistent with createKey/showKey).
	if err := vault.ValidateKeyPath(req.KeyPath); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hrp := req.HRP
	if hrp == "" {
		hrp = "cosmos"
	}
	var sig, pub []byte
	var err error
	switch req.SignMode {
	case "DIRECT":
		var doc []byte
		doc, err = base64.StdEncoding.DecodeString(req.SignDoc)
		if err == nil {
			allowed, status, authzErr := s.authorizeChain(r.Context(), req.KeyPath, apptypes.ChainCosmos)
			if !s.writeChainAuthzResult(w, r, req.KeyPath, apptypes.ChainCosmos, allowed, status, authzErr) {
				return
			}
			sig, pub, err = s.cosmos.SignDirect(r.Context(), req.KeyPath, doc)
		}
	case "AMINO_JSON":
		allowed, status, authzErr := s.authorizeChain(r.Context(), req.KeyPath, apptypes.ChainCosmos)
		if !s.writeChainAuthzResult(w, r, req.KeyPath, apptypes.ChainCosmos, allowed, status, authzErr) {
			return
		}
		sig, pub, err = s.cosmos.SignAmino(r.Context(), req.KeyPath, []byte(req.SignDoc))
	default:
		writeError(w, http.StatusBadRequest, "unsupported sign_mode")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "Cosmos signing failed", "error", err, "key_path", req.KeyPath, "sign_mode", req.SignMode)
		if errors.Is(err, apptypes.ErrBadRequest) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "signing failed")
		return
	}
	addr, derr := cosmossigner.DeriveCosmosAddressFromCompressed(pub, hrp)
	if derr != nil {
		slog.ErrorContext(r.Context(), "cosmos address derivation failed", "error", derr)
		writeError(w, http.StatusInternalServerError, "derive cosmos address: "+derr.Error())
		return
	}
	writeJSON(w, apptypes.SignResponse{
		Signature:     base64.StdEncoding.EncodeToString(sig),
		PubKey:        base64.StdEncoding.EncodeToString(pub),
		CosmosAddress: addr,
	})
}

// createKey godoc
// @Summary Create a KMS key
// @Tags keys
// @Accept json
// @Produce json
// @Param body body apptypes.KeyCreateRequest true "Key create payload"
// @Success 200 {object} apptypes.KeyCreateResponse
// @Failure 400 {object} apptypes.ErrorResponse
// @Failure 401 {object} apptypes.ErrorResponse
// @Failure 403 {object} apptypes.ErrorResponse
// @Failure 429 {object} apptypes.ErrorResponse
// @Failure 500 {object} apptypes.ErrorResponse
// @Security BearerAuth
// @Router /v1/keys [post]
func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req apptypes.KeyCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if err := vault.ValidateKeyPath(req.Path); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rawChains := make([]string, len(req.Chains))
	for i, chain := range req.Chains {
		rawChains[i] = string(chain)
	}
	chains, err := apptypes.ParseChains(rawChains)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	alreadyExisted := false
	if _, err := s.keys.GetPublicKey(r.Context(), req.Path); err == nil {
		alreadyExisted = true
	} else if !errors.Is(err, apptypes.ErrNotFound) {
		s.writeVaultErr(w, r, err, req.Path, "GetPublicKey")
		return
	}

	createChains := make([]string, len(chains))
	for i, chain := range chains {
		createChains[i] = string(chain)
	}
	if err := s.keys.CreateKey(r.Context(), req.Path, createChains); err != nil {
		s.writeVaultErr(w, r, err, req.Path, "CreateKey")
		return
	}

	info, err := keyinfo.For(r.Context(), s.keys, req.Path, keyinfo.DefaultHRP, chains)
	if err != nil {
		s.writeVaultErr(w, r, err, req.Path, "deriveKeyInfo")
		return
	}
	status := http.StatusOK
	if !alreadyExisted {
		status = http.StatusCreated
	}
	writeJSONStatus(w, status, apptypes.KeyCreateResponse{KeyInfo: info, AlreadyExisted: alreadyExisted})
}

// showKey godoc
// @Summary Show a KMS key
// @Tags keys
// @Produce json
// @Param path query string true "Key path (format: {project}/{environment}/{username})" example(proj-a/prod/alice)
// @Success 200 {object} apptypes.KeyInfo
// @Failure 400 {object} apptypes.ErrorResponse
// @Failure 401 {object} apptypes.ErrorResponse
// @Failure 403 {object} apptypes.ErrorResponse
// @Failure 404 {object} apptypes.ErrorResponse
// @Failure 429 {object} apptypes.ErrorResponse
// @Failure 500 {object} apptypes.ErrorResponse
// @Security BearerAuth
// @Router /v1/keys/info [get]
func (s *Server) showKey(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if err := vault.ValidateKeyPath(path); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rawChains, err := s.keys.GetKeyChains(r.Context(), path)
	if err != nil {
		s.writeVaultErr(w, r, err, path, "GetKeyChains")
		return
	}
	chains, err := canonicalizeChains(rawChains)
	if err != nil {
		slog.ErrorContext(r.Context(), "persisted chains are not canonical",
			"error", err, "key_path", path)
		writeError(w, http.StatusInternalServerError, "invalid persisted chains")
		return
	}
	info, err := keyinfo.For(r.Context(), s.keys, path, keyinfo.DefaultHRP, chains)
	if err != nil {
		s.writeVaultErr(w, r, err, path, "GetPublicKey")
		return
	}
	writeJSON(w, info)
}

// updateKeyChains godoc
// @Summary Expand a KMS key's chain allow-list
// @Tags keys
// @Accept json
// @Produce json
// @Param path path string true "Key path (format: {project}/{environment}/{username})" example(proj-a/prod/alice)
// @Param body body apptypes.KeyUpdateChainsRequest true "Chain expansion payload"
// @Success 200 {object} apptypes.KeyUpdateChainsResponse
// @Failure 400 {object} apptypes.ErrorResponse
// @Failure 401 {object} apptypes.ErrorResponse
// @Failure 403 {object} apptypes.ErrorResponse
// @Failure 429 {object} apptypes.ErrorResponse
// @Failure 500 {object} apptypes.ErrorResponse
// @Security BearerAuth
// @Router /v1/keys/{path} [patch]
func (s *Server) updateKeyChains(w http.ResponseWriter, r *http.Request) {
	path := r.PathValue("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if err := vault.ValidateKeyPath(path); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, "only add_chains is supported")
		return
	}
	if len(raw) != 1 {
		writeError(w, http.StatusBadRequest, "only add_chains is supported")
		return
	}
	addRaw, ok := raw["add_chains"]
	if !ok {
		writeError(w, http.StatusBadRequest, "only add_chains is supported")
		return
	}

	var addChains []string
	if err := json.Unmarshal(addRaw, &addChains); err != nil {
		writeError(w, http.StatusBadRequest, "add_chains must be an array of strings")
		return
	}
	parsed, err := apptypes.ParseChains(addChains)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rawChains := make([]string, len(parsed))
	for i, chain := range parsed {
		rawChains[i] = string(chain)
	}
	updated, err := s.keys.UpdateKeyChains(r.Context(), path, rawChains)
	if err != nil {
		s.writeVaultErr(w, r, err, path, "UpdateKeyChains")
		return
	}
	if s.chains != nil {
		s.chains.invalidate(path)
	}
	chains, err := canonicalizeChains(updated)
	if err != nil {
		slog.ErrorContext(r.Context(), "persisted chains are not canonical",
			"error", err, "key_path", path)
		writeError(w, http.StatusInternalServerError, "invalid persisted chains")
		return
	}
	writeJSON(w, apptypes.KeyUpdateChainsResponse{Path: path, Chains: chains})
}

// listKeys godoc
// @Summary List KMS keys by prefix
// @Tags keys
// @Produce json
// @Param prefix query string false "Optional path prefix" example(proj-a/)
// @Success 200 {object} apptypes.KeyListResponse
// @Failure 401 {object} apptypes.ErrorResponse
// @Failure 403 {object} apptypes.ErrorResponse
// @Failure 429 {object} apptypes.ErrorResponse
// @Failure 500 {object} apptypes.ErrorResponse
// @Security BearerAuth
// @Router /v1/keys [get]
func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	limit, err := parseListLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	offset, cursorPrefix, err := parseListCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}
	if cursorPrefix != "" && cursorPrefix != prefix {
		// A cursor encodes the prefix it was issued against; clients passing
		// a mismatched (prefix, cursor) pair almost certainly have a bug.
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}
	ks, err := s.keys.ListKeys(r.Context(), prefix)
	if err != nil {
		s.writeVaultErr(w, r, err, prefix, "ListKeys")
		return
	}
	if ks == nil {
		ks = []string{}
	}
	// Client-side pagination: fetch the full list, slice. Documented in
	// design.md as a first-step implementation that can be swapped for
	// plugin-native pagination later without changing the wire shape.
	if offset > len(ks) {
		offset = len(ks)
	}
	end := offset + limit
	if end > len(ks) {
		end = len(ks)
	}
	page := append([]string{}, ks[offset:end]...)
	entries := make([]apptypes.KeyListEntry, len(page))
	for i, name := range page {
		// Vault's LIST returns names relative to the prefix; rejoin so entry.Path
		// is the fully-qualified key path and the chains lookup below (which needs
		// {project}/{environment}/{username}) targets the right key.
		entries[i].Path = joinKeyPath(prefix, name)
		// Default to an empty array (never null) so the wire schema holds even
		// when the chain tag read fails below; ChainsAvailable stays false.
		entries[i].Chains = []apptypes.Chain{}
	}
	if len(page) > 0 {
		jobs := make(chan listKeyChainsJob)
		workers := listKeyChainsWorkers
		if len(page) < workers {
			workers = len(page)
		}
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range jobs {
					ctx, cancel := context.WithTimeout(r.Context(), listKeyChainsTimeout)
					chains, err := s.keys.GetKeyChains(ctx, job.path)
					cancel()
					if err == nil {
						if parsed, ok := toKeyListChains(chains); ok {
							entries[job.index].Chains = parsed
							entries[job.index].ChainsAvailable = true
						}
					}
				}
			}()
		}
		for i := range page {
			jobs <- listKeyChainsJob{index: i, path: entries[i].Path}
		}
		close(jobs)
		wg.Wait()
	}
	next := ""
	if end < len(ks) {
		next = encodeListCursor(prefix, end)
	}
	writeJSON(w, apptypes.KeyListResponse{Keys: entries, Count: len(entries), NextCursor: next})
}

const (
	listLimitDefault     = 100
	listLimitMax         = 1000
	listKeyChainsWorkers = 8
	listKeyChainsTimeout = 2 * time.Second
)

type listKeyChainsJob struct {
	index int
	path  string
}

// toKeyListChains canonicalizes the raw persisted chains for a list entry.
// The bool is false when the strings cannot be parsed, so the caller can mark
// the entry's chains unavailable rather than reporting a bogus allow-list.
func toKeyListChains(chains []string) ([]apptypes.Chain, bool) {
	out, err := canonicalizeChains(chains)
	if err != nil {
		return nil, false
	}
	return out, true
}

// joinKeyPath reassembles the fully-qualified key path from the list prefix and
// a name returned by ListKeys. Vault's LIST returns names relative to the
// prefix, so the bare name (e.g. "alice") is not a valid key path on its own.
func joinKeyPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	if strings.HasSuffix(prefix, "/") {
		return prefix + name
	}
	return prefix + "/" + name
}

// parseListLimit parses ?limit and clamps it to [1, listLimitMax]. Empty
// string → listLimitDefault. Negative or non-numeric returns an error so
// clients see "limit must be a positive integer".
func parseListLimit(raw string) (int, error) {
	if raw == "" {
		return listLimitDefault, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("limit must be a positive integer")
	}
	if n < 1 {
		return listLimitDefault, nil
	}
	if n > listLimitMax {
		return listLimitMax, nil
	}
	return n, nil
}

type listCursorPayload struct {
	Prefix string `json:"p"`
	Offset int    `json:"o"`
}

func encodeListCursor(prefix string, offset int) string {
	b, _ := json.Marshal(listCursorPayload{Prefix: prefix, Offset: offset})
	return base64.URLEncoding.EncodeToString(b)
}

func parseListCursor(raw string) (offset int, prefix string, err error) {
	if raw == "" {
		return 0, "", nil
	}
	decoded, derr := base64.URLEncoding.DecodeString(raw)
	if derr != nil {
		return 0, "", derr
	}
	var p listCursorPayload
	if jerr := json.Unmarshal(decoded, &p); jerr != nil {
		return 0, "", jerr
	}
	if p.Offset < 0 {
		return 0, "", errors.New("negative cursor offset")
	}
	return p.Offset, p.Prefix, nil
}

func (s *Server) writeVaultErr(w http.ResponseWriter, r *http.Request, err error, keyPath, op string) {
	slog.ErrorContext(r.Context(), "key operation failed", "error", err, "key_path", keyPath, "op", op)
	switch {
	case errors.Is(err, apptypes.ErrNotFound):
		writeError(w, http.StatusNotFound, "key not found: "+keyPath)
	case errors.Is(err, apptypes.ErrPermission):
		writeError(w, http.StatusForbidden, "permission denied")
	case errors.Is(err, apptypes.ErrBadRequest):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "vault error")
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONStatus is like writeJSON but writes an explicit status code. Used
// by handlers that need a non-200 success code (e.g. 201 Created).
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apptypes.ErrorResponse{Error: msg})
}

func decodeHex(s string) ([]byte, error) {
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}

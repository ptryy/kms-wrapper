package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	httpSwagger "github.com/swaggo/http-swagger/v2"
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
	CreateKey(ctx context.Context, path string) error
	GetPublicKey(ctx context.Context, path string) ([]byte, error)
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

type Server struct {
	cfg          config.Config
	vault        HealthChecker
	keys         KeyStore
	evm          EVMSigner
	cosmos       CosmosSigner
	server       *http.Server
	limiter      *rate.Limiter
	expectedAuth string
}

func New(cfg config.Config, vault HealthChecker, keys KeyStore, evmSigner EVMSigner, cosmosSigner CosmosSigner) *Server {
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
	s := &Server{
		cfg:          cfg,
		vault:        vault,
		keys:         keys,
		evm:          evmSigner,
		cosmos:       cosmosSigner,
		limiter:      rate.NewLimiter(rate.Limit(rl), burst),
		expectedAuth: "Bearer " + cfg.Gateway.Token,
	}
	s.server = &http.Server{
		Addr:              cfg.Gateway.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return s
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

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.Handle("POST /sign/evm", s.rateLimit(s.auth(http.HandlerFunc(s.signEVM))))
	mux.Handle("POST /sign/cosmos", s.rateLimit(s.auth(http.HandlerFunc(s.signCosmos))))
	mux.Handle("POST /keys", s.rateLimit(s.auth(http.HandlerFunc(s.createKey))))
	mux.Handle("GET /keys/info", s.rateLimit(s.auth(http.HandlerFunc(s.showKey))))
	mux.Handle("GET /keys", s.rateLimit(s.auth(http.HandlerFunc(s.listKeys))))
	if s.cfg.Gateway.SwaggerEnabled {
		var swagger http.Handler = httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json"))
		if s.cfg.Gateway.SwaggerAuth {
			swagger = s.auth(swagger)
		}
		mux.Handle("GET /swagger/", swagger)
	}
	return s.requestLogger(mux)
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

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.limiter.Allow() {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.expectedAuth)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// health godoc
// @Summary Gateway health status
// @Tags health
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Security
// @Router /health [get]
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.vault != nil && s.vault.Health() == nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "vault": "reachable"})
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "degraded", "vault": "unreachable"})
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
// @Router /sign/evm [post]
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
	payloads := countNonEmpty(req.RawTx, req.PersonalMessage, req.EIP712Digest)
	if payloads == 0 {
		writeError(w, http.StatusBadRequest, "payload is required")
		return
	}
	if payloads > 1 {
		writeError(w, http.StatusBadRequest, "only one payload field is allowed")
		return
	}

	if req.RawTx != "" {
		if req.ChainID <= 0 {
			writeError(w, http.StatusBadRequest, "chain_id is required and must be positive")
			return
		}
		raw, err := decodeHex(req.RawTx)
		if err != nil {
			writeError(w, http.StatusBadRequest, "raw_tx must be hex")
			return
		}
		out, err := s.evm.SignRawTx(r.Context(), req.KeyPath, big.NewInt(req.ChainID), raw)
		if err != nil {
			slog.ErrorContext(r.Context(), "EVM raw tx signing failed", "error", err, "key_path", req.KeyPath)
			writeError(w, http.StatusInternalServerError, "signing failed")
			return
		}
		var tx ethtypes.Transaction
		_ = tx.UnmarshalBinary(out)
		v, rpart, spart := tx.RawSignatureValues()
		writeJSON(w, apptypes.SignResponse{
			SignedTx: "0x" + hex.EncodeToString(out),
			Parts:    &apptypes.SignatureParts{R: rpart.Text(16), S: spart.Text(16), V: v.Uint64()},
		})
		return
	}

	if req.PersonalMessage != "" {
		msg, err := decodeHex(req.PersonalMessage)
		if err != nil {
			writeError(w, http.StatusBadRequest, "personal_message must be hex")
			return
		}
		sig, err := s.evm.SignPersonalMessage(r.Context(), req.KeyPath, msg)
		if err != nil {
			slog.ErrorContext(r.Context(), "personal message signing failed", "error", err, "key_path", req.KeyPath)
			writeError(w, http.StatusInternalServerError, "signing failed")
			return
		}
		// eth_sign / personal_sign expects v=27/28
		writeJSON(w, apptypes.SignResponse{Signature: "0x" + hex.EncodeToString(evm.NormalizeEthereumV(sig))})
		return
	}

	// EIP-712: validate early, return raw v=0/1 (no +27 offset per spec)
	digest, err := decodeHex(req.EIP712Digest)
	if err != nil {
		writeError(w, http.StatusBadRequest, "eip712_digest must be hex")
		return
	}
	if len(digest) != 32 {
		writeError(w, http.StatusBadRequest, "eip712_digest must be exactly 32 bytes")
		return
	}
	sig, err := s.evm.SignEIP712Digest(r.Context(), req.KeyPath, digest)
	if err != nil {
		slog.ErrorContext(r.Context(), "EIP-712 signing failed", "error", err, "key_path", req.KeyPath)
		writeError(w, http.StatusInternalServerError, "signing failed")
		return
	}
	writeJSON(w, apptypes.SignResponse{Signature: "0x" + hex.EncodeToString(sig)})
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
// @Router /sign/cosmos [post]
func (s *Server) signCosmos(w http.ResponseWriter, r *http.Request) {
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
			sig, pub, err = s.cosmos.SignDirect(r.Context(), req.KeyPath, doc)
		}
	case "AMINO_JSON":
		sig, pub, err = s.cosmos.SignAmino(r.Context(), req.KeyPath, []byte(req.SignDoc))
	default:
		writeError(w, http.StatusBadRequest, "unsupported sign_mode")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "Cosmos signing failed", "error", err, "key_path", req.KeyPath, "sign_mode", req.SignMode)
		writeError(w, http.StatusInternalServerError, "signing failed")
		return
	}
	addr, _ := cosmossigner.DeriveCosmosAddressFromCompressed(pub, hrp)
	writeJSON(w, apptypes.SignResponse{
		Signature:     base64.StdEncoding.EncodeToString(sig),
		PubKey:        base64.StdEncoding.EncodeToString(pub),
		CosmosAddress: addr,
	})
}

// createKey godoc
// @Summary Create a Vault Transit key
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
// @Router /keys [post]
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

	alreadyExisted := false
	if _, err := s.keys.GetPublicKey(r.Context(), req.Path); err == nil {
		alreadyExisted = true
	} else if !errors.Is(err, apptypes.ErrNotFound) {
		s.writeVaultErr(w, r, err, req.Path, "GetPublicKey")
		return
	}

	if err := s.keys.CreateKey(r.Context(), req.Path); err != nil {
		s.writeVaultErr(w, r, err, req.Path, "CreateKey")
		return
	}

	info, err := keyinfo.For(r.Context(), s.keys, req.Path, keyinfo.DefaultHRP)
	if err != nil {
		s.writeVaultErr(w, r, err, req.Path, "deriveKeyInfo")
		return
	}
	writeJSON(w, apptypes.KeyCreateResponse{KeyInfo: info, AlreadyExisted: alreadyExisted})
}

// showKey godoc
// @Summary Show a Vault Transit key
// @Tags keys
// @Produce json
// @Param path query string true "Key path (format: {project}/{chain}/{username})" example(proj-a/evm/alice)
// @Success 200 {object} apptypes.KeyInfo
// @Failure 400 {object} apptypes.ErrorResponse
// @Failure 401 {object} apptypes.ErrorResponse
// @Failure 403 {object} apptypes.ErrorResponse
// @Failure 404 {object} apptypes.ErrorResponse
// @Failure 429 {object} apptypes.ErrorResponse
// @Failure 500 {object} apptypes.ErrorResponse
// @Security BearerAuth
// @Router /keys/info [get]
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
	info, err := keyinfo.For(r.Context(), s.keys, path, keyinfo.DefaultHRP)
	if err != nil {
		s.writeVaultErr(w, r, err, path, "GetPublicKey")
		return
	}
	writeJSON(w, info)
}

// listKeys godoc
// @Summary List Vault Transit keys by prefix
// @Tags keys
// @Produce json
// @Param prefix query string false "Optional path prefix" example(proj-a/)
// @Success 200 {object} apptypes.KeyListResponse
// @Failure 401 {object} apptypes.ErrorResponse
// @Failure 403 {object} apptypes.ErrorResponse
// @Failure 429 {object} apptypes.ErrorResponse
// @Failure 500 {object} apptypes.ErrorResponse
// @Security BearerAuth
// @Router /keys [get]
func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	ks, err := s.keys.ListKeys(r.Context(), prefix)
	if err != nil {
		s.writeVaultErr(w, r, err, prefix, "ListKeys")
		return
	}
	if ks == nil {
		ks = []string{}
	}
	writeJSON(w, apptypes.KeyListResponse{Keys: ks, Count: len(ks)})
}

func (s *Server) writeVaultErr(w http.ResponseWriter, r *http.Request, err error, keyPath, op string) {
	slog.ErrorContext(r.Context(), "key operation failed", "error", err, "key_path", keyPath, "op", op)
	switch {
	case errors.Is(err, apptypes.ErrNotFound):
		writeError(w, http.StatusNotFound, "key not found: "+keyPath)
	case errors.Is(err, apptypes.ErrPermission):
		writeError(w, http.StatusForbidden, "permission denied")
	default:
		writeError(w, http.StatusInternalServerError, "vault error")
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
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

func countNonEmpty(vals ...string) int {
	n := 0
	for _, v := range vals {
		if v != "" {
			n++
		}
	}
	return n
}

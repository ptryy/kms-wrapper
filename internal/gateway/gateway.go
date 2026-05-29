package gateway

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/ryan-truong/kms-wrapper/internal/config"
	"github.com/ryan-truong/kms-wrapper/internal/signer/evm"
	apptypes "github.com/ryan-truong/kms-wrapper/pkg/types"
)

type HealthChecker interface{ Health() error }

type EVMSigner interface {
	SignRawTx(keyPath string, chainID *big.Int, rawTx []byte) ([]byte, error)
	SignPersonalMessage(keyPath string, msg []byte) ([]byte, error)
	SignEIP712Digest(keyPath string, digest []byte) ([]byte, error)
}

type CosmosSigner interface {
	SignDirect(keyPath string, signDocBytes []byte) ([]byte, []byte, error)
	SignAmino(keyPath string, stdSignDocJSON []byte) ([]byte, []byte, error)
}

type Server struct {
	cfg    config.Config
	vault  HealthChecker
	evm    EVMSigner
	cosmos CosmosSigner
	server *http.Server
}

func New(cfg config.Config, vault HealthChecker, evmSigner EVMSigner, cosmosSigner CosmosSigner) *Server {
	if cfg.Gateway.Addr == "" {
		cfg.Gateway.Addr = "127.0.0.1:8080"
	}
	s := &Server{cfg: cfg, vault: vault, evm: evmSigner, cosmos: cosmosSigner}
	s.server = &http.Server{Addr: cfg.Gateway.Addr, Handler: s.routes()}
	return s
}

func (s *Server) ListenAndServe() error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	mux.Handle("POST /sign/evm", s.auth(http.HandlerFunc(s.signEVM)))
	mux.Handle("POST /sign/cosmos", s.auth(http.HandlerFunc(s.signCosmos)))
	return mux
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.cfg.Gateway.Token {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.vault != nil && s.vault.Health() == nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "vault": "reachable"})
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "degraded", "vault": "unreachable"})
}

func (s *Server) signEVM(w http.ResponseWriter, r *http.Request) {
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
		raw, err := decodeHex(req.RawTx)
		if err != nil {
			writeError(w, http.StatusBadRequest, "raw_tx must be hex")
			return
		}
		out, err := s.evm.SignRawTx(req.KeyPath, big.NewInt(req.ChainID), raw)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var tx ethtypes.Transaction
		_ = tx.UnmarshalBinary(out)
		v, rpart, spart := tx.RawSignatureValues()
		writeJSON(w, apptypes.SignResponse{SignedTx: "0x" + hex.EncodeToString(out), Parts: &apptypes.SignatureParts{R: rpart.Text(16), S: spart.Text(16), V: uint8(v.Uint64())}})
		return
	}
	var input []byte
	var err error
	if req.PersonalMessage != "" {
		input, err = decodeHex(req.PersonalMessage)
		if err == nil {
			input, err = s.evm.SignPersonalMessage(req.KeyPath, input)
		}
	} else {
		input, err = decodeHex(req.EIP712Digest)
		if err == nil {
			input, err = s.evm.SignEIP712Digest(req.KeyPath, input)
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, apptypes.SignResponse{Signature: "0x" + hex.EncodeToString(evm.NormalizeEthereumV(input))})
}

func (s *Server) signCosmos(w http.ResponseWriter, r *http.Request) {
	var req apptypes.CosmosSignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.KeyPath == "" {
		writeError(w, http.StatusBadRequest, "key_path is required")
		return
	}
	var sig, pub []byte
	var err error
	switch req.SignMode {
	case "DIRECT":
		var doc []byte
		doc, err = base64.StdEncoding.DecodeString(req.SignDoc)
		if err == nil {
			sig, pub, err = s.cosmos.SignDirect(req.KeyPath, doc)
		}
	case "AMINO_JSON":
		sig, pub, err = s.cosmos.SignAmino(req.KeyPath, []byte(req.SignDoc))
	default:
		writeError(w, http.StatusBadRequest, "unsupported sign_mode")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, apptypes.SignResponse{Signature: base64.StdEncoding.EncodeToString(sig), PubKey: base64.StdEncoding.EncodeToString(pub)})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
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

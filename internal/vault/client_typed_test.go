package vault

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/ryan-truong/kms-wrapper/pkg/types"
)

// newTypedClient produces a *Client wired to an httptest server that
// answers /v1/sys/health with 200 (so NewClient's startup probe passes)
// and routes everything else to the supplied handler.
func newTypedClient(t *testing.T, app http.HandlerFunc) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sys/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"initialized":true,"sealed":false,"standby":false}`))
			return
		}
		app(w, r)
	}))
	c, err := NewClient(srv.URL, TokenAuthProvider{TokenValue: "test-token"})
	if err != nil {
		srv.Close()
		t.Fatalf("NewClient: %v", err)
	}
	return c, srv.Close
}

func writeVaultJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"errors":["` + msg + `"]}`))
}

func TestMapVaultErrViaResponseError(t *testing.T) {
	const keyPath = "proj/prod/alice"

	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		// Status text deliberately does NOT contain the legacy substring matchers
		// would have picked up — typed mapping must classify by code, not text.
		{"403 with reworded body", http.StatusForbidden, "vault said no", types.ErrPermission},
		{"404 with reworded body", http.StatusNotFound, "absent", types.ErrNotFound},
		{"400 with reworded body", http.StatusBadRequest, "malformed payload", types.ErrBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, cleanup := newTypedClient(t, func(w http.ResponseWriter, _ *http.Request) {
				writeVaultJSONError(w, tc.status, tc.body)
			})
			defer cleanup()
			_, err := c.GetPublicKey(context.Background(), keyPath)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("errors.Is(%v, %v) = false (err=%v)", err, tc.want, err)
			}
		})
	}
}

func TestSignMaps403ChainMismatchToPermission(t *testing.T) {
	c, cleanup := newTypedClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/kms/sign/") {
			writeVaultJSONError(w, http.StatusForbidden, "not authorized for cosmos signing")
			return
		}
		http.NotFound(w, r)
	})
	defer cleanup()

	_, _, err := c.Sign(context.Background(), "proj/prod/alice", make([]byte, 32), "cosmos")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, types.ErrPermission) {
		t.Fatalf("errors.Is(%v, %v) = false", err, types.ErrPermission)
	}
	if !strings.Contains(err.Error(), "not authorized for cosmos signing") {
		t.Fatalf("expected preserved message, got %v", err)
	}
}

func TestGetPublicKeyCachesPerPath(t *testing.T) {
	key, _ := crypto.GenerateKey()
	compressed := crypto.CompressPubkey(&key.PublicKey)
	var calls atomic.Int64

	c, cleanup := newTypedClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/kms/keys/") {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"compressed_pub_key":"` +
				base64.StdEncoding.EncodeToString(compressed) + `"}}`))
			return
		}
		http.NotFound(w, r)
	})
	defer cleanup()

	const path = "proj/prod/alice"
	a, err := c.GetPublicKey(context.Background(), path)
	if err != nil {
		t.Fatalf("first GetPublicKey: %v", err)
	}
	b, err := c.GetPublicKey(context.Background(), path)
	if err != nil {
		t.Fatalf("second GetPublicKey: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("cached pubkey differs from first fetch")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 underlying HTTP call, got %d", got)
	}

	// Different path triggers another fetch.
	if _, err := c.GetPublicKey(context.Background(), "proj/prod/bob"); err != nil {
		t.Fatalf("second-path GetPublicKey: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 HTTP calls for two distinct paths, got %d", got)
	}
}

func TestRenewalLogsWarnOnFailure(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	c, cleanup := newTypedClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token/lookup-self") {
			writeVaultJSONError(w, http.StatusForbidden, "no")
			return
		}
		http.NotFound(w, r)
	})
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	go c.renewalLoop(ctx)
	// Wait for the first backoff iteration to log; cancel before the next
	// 2-second backoff fires so the test finishes promptly.
	time.Sleep(200 * time.Millisecond) //nolint:revive // brief settle is intentional
	cancel()

	if !strings.Contains(buf.String(), "vault token lookup failed") {
		t.Fatalf("expected warn log line, got: %s", buf.String())
	}
}

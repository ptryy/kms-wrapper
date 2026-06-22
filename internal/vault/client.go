package vault

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	vaultapi "github.com/hashicorp/vault/api"

	"github.com/ryan-truong/kms-wrapper/pkg/types"
)

var ErrNotImplemented = errors.New("not implemented")

type AuthProvider interface {
	Token() (string, error)
}

type TokenAuthProvider struct {
	TokenValue string
}

func (p TokenAuthProvider) Token() (string, error) {
	if p.TokenValue == "" {
		return "", errors.New("vault token is required")
	}
	return p.TokenValue, nil
}

// AppRoleAuthProvider is reserved for future AppRole authentication.
// It is not yet implemented; NewClient returns an explicit error when it is used.
type AppRoleAuthProvider struct{}

func (AppRoleAuthProvider) Token() (string, error) { return "", ErrNotImplemented }

type Client struct {
	api          *vaultapi.Client
	addr         string
	pubkeyCache  sync.Map
	lastLookupNs atomic.Int64
	// onRenewalFailure is invoked once per LookupSelf/RenewSelf error in the
	// renewal goroutine; observability layers (kms_token_renewal_failures_total)
	// wire it up. Nil-safe.
	onRenewalFailure func()
	onVaultCall      VaultCallObserver
}

// SetRenewalFailureHook installs a callback invoked once per renewal error.
// It is intended for the observability change to increment a Prometheus
// counter; the vault package does not depend on prometheus.
func (c *Client) SetRenewalFailureHook(fn func()) { c.onRenewalFailure = fn }

// VaultCallObserver records a Vault call's outcome. `status` is one of
// "ok", "permission_denied", "not_found", "error". seconds is wall-clock
// duration. Hook is set via SetVaultCallObserver; the vault package does
// not import prometheus directly.
type VaultCallObserver func(op, status string, seconds float64)

// SetVaultCallObserver installs the per-call instrumentation hook. Nil-safe.
func (c *Client) SetVaultCallObserver(fn VaultCallObserver) { c.onVaultCall = fn }

func (c *Client) recordCall(op string, start time.Time, err error) {
	if c.onVaultCall == nil {
		return
	}
	status := "ok"
	switch {
	case err == nil:
	case errors.Is(err, types.ErrPermission):
		status = "permission_denied"
	case errors.Is(err, types.ErrNotFound):
		status = "not_found"
	default:
		status = "error"
	}
	c.onVaultCall(op, status, time.Since(start).Seconds())
}

// LastLookupSelf returns the wall-clock time of the most recent successful
// `LookupSelf`. The zero time means "no successful lookup since startup".
// Readers (e.g. /readyz) should treat a stale timestamp as not-ready.
func (c *Client) LastLookupSelf() time.Time {
	ns := c.lastLookupNs.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func NewClient(addr string, auth AuthProvider) (*Client, error) {
	if addr == "" {
		return nil, errors.New("vault addr is required")
	}
	token, err := auth.Token()
	if err != nil {
		if errors.Is(err, ErrNotImplemented) {
			return nil, errors.New("AppRoleAuthProvider is not yet implemented; use TokenAuthProvider")
		}
		return nil, err
	}
	cfg := vaultapi.DefaultConfig()
	cfg.Address = addr
	apiClient, err := vaultapi.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	apiClient.SetToken(token)
	c := &Client{api: apiClient, addr: addr}
	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<(attempt-1)) * time.Second)
		}
		if err := c.Health(); err == nil {
			return c, nil
		} else {
			lastErr = err
		}
	}
	return nil, fmt.Errorf("vault unreachable at %s after 3 attempts: %w", addr, lastErr)
}

// StartRenewal starts a background goroutine that renews the Vault token
// before expiry. The initial LookupSelf is retried with capped exponential
// backoff (1, 2, 4, 8, 16, 30 s, then steady 30 s) until it succeeds or
// ctx is cancelled. The renewal cadence is recomputed after each successful
// LookupSelf as max(30s, ttl/3) so a short-lived token does not expire
// between ticks. Errors are logged at warn; successful renewals at debug.
func (c *Client) StartRenewal(ctx context.Context) {
	go c.renewalLoop(ctx)
}

func (c *Client) renewalLoop(ctx context.Context) {
	backoffs := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second}
	var info *vaultapi.Secret
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return
		}
		got, err := c.api.Auth().Token().LookupSelf()
		if err == nil && got != nil {
			info = got
			c.lastLookupNs.Store(time.Now().UnixNano())
			break
		}
		slog.WarnContext(ctx, "vault token lookup failed", "err", err, "attempt", attempt+1)
		c.invokeRenewalFailure()
		delay := backoffs[len(backoffs)-1]
		if attempt < len(backoffs) {
			delay = backoffs[attempt]
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
	renewable, _ := info.Data["renewable"].(bool)
	if !renewable {
		return
	}
	interval := renewalInterval(info)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewed, err := c.api.Auth().Token().RenewSelf(0)
			if err != nil {
				slog.WarnContext(ctx, "vault token renewal failed", "err", err)
				c.invokeRenewalFailure()
				continue
			}
			latest, lerr := c.api.Auth().Token().LookupSelf()
			if lerr != nil || latest == nil {
				slog.WarnContext(ctx, "vault token lookup after renew failed", "err", lerr)
				c.invokeRenewalFailure()
				continue
			}
			c.lastLookupNs.Store(time.Now().UnixNano())
			ttlSeconds, _ := latest.Data["ttl"].(int64)
			slog.DebugContext(ctx, "vault token renewed", "ttl_s", ttlSeconds, "lease_id", renewed.LeaseID)
			next := renewalInterval(latest)
			if next != interval {
				interval = next
				ticker.Reset(interval)
			}
		}
	}
}

func (c *Client) invokeRenewalFailure() {
	if c.onRenewalFailure != nil {
		c.onRenewalFailure()
	}
}

// renewalInterval returns max(30s, ttl/3) for the supplied secret's `ttl`
// field. Vault returns the TTL as a json.Number → float64 through the api
// client; defensive handling covers both shapes.
func renewalInterval(info *vaultapi.Secret) time.Duration {
	floor := 30 * time.Second
	if info == nil || info.Data == nil {
		return floor
	}
	var ttlSec float64
	switch v := info.Data["ttl"].(type) {
	case float64:
		ttlSec = v
	case int:
		ttlSec = float64(v)
	case int64:
		ttlSec = float64(v)
	}
	candidate := time.Duration(ttlSec/3) * time.Second
	if candidate < floor {
		return floor
	}
	return candidate
}

func (c *Client) Health() error {
	start := time.Now()
	_, err := c.api.Sys().Health()
	c.recordCall("health", start, err)
	return err
}

// CreateKey requests the kms-vault-plugin to generate a new secp256k1 key at
// the given path. Idempotent — plugin returns the existing key info if it
// already exists.
func (c *Client) CreateKey(ctx context.Context, path string, chains []string) error {
	if err := ValidateKeyPath(path); err != nil {
		return err
	}
	start := time.Now()
	_, err := c.api.Logical().WriteWithContext(ctx, ToVaultPath(path), map[string]any{
		"chains": strings.Join(chains, ","),
	})
	mapped := mapVaultErr(path, err)
	c.recordCall("create", start, mapped)
	return mapped
}

// GetPublicKey returns the 65-byte uncompressed secp256k1 public key for the
// given key path. The plugin returns a compressed (33-byte) key; we decompress
// here so downstream signers continue to receive the uncompressed form.
// Results are cached in pubkeyCache for the lifetime of the process —
// secp256k1 pubkeys are immutable for a stored private key, so no invalidation
// is needed.
func (c *Client) GetPublicKey(ctx context.Context, path string) ([]byte, error) {
	if err := ValidateKeyPath(path); err != nil {
		return nil, err
	}
	if cached, ok := c.pubkeyCache.Load(path); ok {
		// Return a copy so callers can freely mutate the result (e.g. signers
		// that need the uncompressed-prefix byte stripped).
		src := cached.([]byte)
		out := make([]byte, len(src))
		copy(out, src)
		return out, nil
	}
	start := time.Now()
	secret, err := c.api.Logical().ReadWithContext(ctx, ToVaultPath(path))
	if err != nil {
		mapped := mapVaultErr(path, err)
		c.recordCall("read", start, mapped)
		return nil, mapped
	}
	if secret == nil || secret.Data == nil {
		err = fmt.Errorf("%w: key not found: %s", types.ErrNotFound, path)
		c.recordCall("read", start, err)
		return nil, err
	}
	c.recordCall("read", start, nil)
	compressedB64, ok := secret.Data["compressed_pub_key"].(string)
	if !ok || compressedB64 == "" {
		return nil, errors.New("plugin response missing compressed_pub_key")
	}
	compressed, err := base64.StdEncoding.DecodeString(compressedB64)
	if err != nil {
		return nil, fmt.Errorf("decode compressed_pub_key: %w", err)
	}
	if len(compressed) != 33 {
		return nil, fmt.Errorf("expected 33-byte compressed pubkey, got %d", len(compressed))
	}
	pub, err := crypto.DecompressPubkey(compressed)
	if err != nil {
		return nil, fmt.Errorf("decompress secp256k1 pubkey: %w", err)
	}
	uncompressed := crypto.FromECDSAPub(pub)
	stored := make([]byte, len(uncompressed))
	copy(stored, uncompressed)
	c.pubkeyCache.Store(path, stored)
	return uncompressed, nil
}

// Sign submits a pre-hashed 32-byte input to the kms-vault-plugin and returns
// the (r, s) components of the resulting low-S-normalised secp256k1 signature.
func (c *Client) Sign(ctx context.Context, path string, hash []byte, chain string) (r, s *big.Int, err error) {
	if err := ValidateKeyPath(path); err != nil {
		return nil, nil, err
	}
	if len(hash) != 32 {
		return nil, nil, errors.New("payload must be 32 bytes (pre-hashed)")
	}
	// Canonicalize so minor formatting (case/whitespace) matches the canonical
	// persisted allow-list instead of being spuriously denied by the plugin.
	chain = strings.ToLower(strings.TrimSpace(chain))
	if chain == "" {
		return nil, nil, errors.New("chain is required")
	}
	start := time.Now()
	secret, err := c.api.Logical().WriteWithContext(ctx, ToSignPath(path), map[string]any{
		"input": hex.EncodeToString(hash),
		"chain": chain,
	})
	if err != nil {
		mapped := mapVaultErr(path, err)
		c.recordCall("sign", start, mapped)
		return nil, nil, mapped
	}
	if secret == nil || secret.Data == nil {
		err = fmt.Errorf("%w: key not found during sign: %s", types.ErrNotFound, path)
		c.recordCall("sign", start, err)
		return nil, nil, err
	}
	c.recordCall("sign", start, nil)
	rHex, _ := secret.Data["r"].(string)
	sHex, _ := secret.Data["s"].(string)
	if rHex == "" || sHex == "" {
		return nil, nil, errors.New("plugin response missing r/s fields")
	}
	rBytes, err := hex.DecodeString(rHex)
	if err != nil {
		return nil, nil, fmt.Errorf("decode signature r: %w", err)
	}
	sBytes, err := hex.DecodeString(sHex)
	if err != nil {
		return nil, nil, fmt.Errorf("decode signature s: %w", err)
	}
	return new(big.Int).SetBytes(rBytes), new(big.Int).SetBytes(sBytes), nil
}

// GetKeyChains reads the persisted chain list for a key path.
func (c *Client) GetKeyChains(ctx context.Context, path string) ([]string, error) {
	if err := ValidateKeyPath(path); err != nil {
		return nil, err
	}
	start := time.Now()
	secret, err := c.api.Logical().ReadWithContext(ctx, ToVaultPath(path))
	if err != nil {
		mapped := mapVaultErr(path, err)
		c.recordCall("read", start, mapped)
		return nil, mapped
	}
	if secret == nil || secret.Data == nil {
		err = fmt.Errorf("%w: key not found: %s", types.ErrNotFound, path)
		c.recordCall("read", start, err)
		return nil, err
	}
	c.recordCall("read", start, nil)
	chains, err := decodeStringSlice(secret.Data["chains"])
	if err != nil {
		return nil, fmt.Errorf("decode chains: %w", err)
	}
	return chains, nil
}

// UpdateKeyChains adds chains to the persisted list and returns the canonical
// list reported by the plugin.
func (c *Client) UpdateKeyChains(ctx context.Context, path string, addChains []string) ([]string, error) {
	if err := ValidateKeyPath(path); err != nil {
		return nil, err
	}
	start := time.Now()
	secret, err := c.api.Logical().WriteWithContext(ctx, ToVaultPath(path), map[string]any{
		"add_chains": strings.Join(addChains, ","),
	})
	if err != nil {
		mapped := mapVaultErr(path, err)
		c.recordCall("update_chains", start, mapped)
		return nil, mapped
	}
	if secret == nil || secret.Data == nil {
		err = fmt.Errorf("%w: key not found: %s", types.ErrNotFound, path)
		c.recordCall("update_chains", start, err)
		return nil, err
	}
	c.recordCall("update_chains", start, nil)
	chains, err := decodeStringSlice(secret.Data["chains"])
	if err != nil {
		return nil, fmt.Errorf("decode chains: %w", err)
	}
	return chains, nil
}

// ListKeys lists key names under the given prefix via the plugin's LIST endpoint.
func (c *Client) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	path := "kms/keys"
	if prefix != "" {
		path = "kms/keys/" + prefix
	}
	start := time.Now()
	secret, err := c.api.Logical().ListWithContext(ctx, path)
	if err != nil {
		mapped := mapVaultErr(prefix, err)
		c.recordCall("list", start, mapped)
		return nil, mapped
	}
	c.recordCall("list", start, nil)
	if secret == nil || secret.Data == nil {
		return nil, nil
	}
	rawKeys, ok := secret.Data["keys"].([]any)
	if !ok {
		return nil, nil
	}
	keys := make([]string, 0, len(rawKeys))
	for _, k := range rawKeys {
		if s, ok := k.(string); ok {
			keys = append(keys, s)
		}
	}
	return keys, nil
}

// mapVaultErr classifies an error from `github.com/hashicorp/vault/api` by
// extracting *vaultapi.ResponseError via errors.As and switching on the HTTP
// StatusCode. Substring matching of the error message is deliberately avoided
// — Vault may reword bodies between versions and reverse-proxies sometimes
// rewrite them. Errors that aren't ResponseError (TCP reset, DNS failure)
// pass through unchanged so callers map them to HTTP 500.
func mapVaultErr(path string, err error) error {
	if err == nil {
		return nil
	}
	var rerr *vaultapi.ResponseError
	if errors.As(err, &rerr) {
		switch rerr.StatusCode {
		case http.StatusForbidden:
			return fmt.Errorf("%w: %s", types.ErrPermission, rerr.Error())
		case http.StatusNotFound:
			return fmt.Errorf("%w: key not found: %s", types.ErrNotFound, path)
		case http.StatusBadRequest:
			return fmt.Errorf("%w: %s", types.ErrBadRequest, strings.Join(rerr.Errors, "; "))
		}
	}
	return err
}

func decodeStringSlice(v any) ([]string, error) {
	switch xs := v.(type) {
	case []string:
		out := make([]string, len(xs))
		copy(out, xs)
		return out, nil
	case []any:
		out := make([]string, 0, len(xs))
		for _, item := range xs {
			s, ok := item.(string)
			if !ok {
				return nil, errors.New("chains contains non-string value")
			}
			out = append(out, s)
		}
		return out, nil
	case nil:
		return nil, errors.New("missing chains field")
	default:
		return nil, fmt.Errorf("unexpected chains type %T", v)
	}
}

package vault

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
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
	api  *vaultapi.Client
	addr string
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

// StartRenewal starts a background goroutine that renews the Vault token before expiry.
// It is a no-op for non-renewable tokens (e.g., root tokens in dev mode).
func (c *Client) StartRenewal(ctx context.Context) {
	info, err := c.api.Auth().Token().LookupSelf()
	if err != nil || info == nil {
		return
	}
	renewable, _ := info.Data["renewable"].(bool)
	if !renewable {
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = c.api.Auth().Token().RenewSelf(0)
			}
		}
	}()
}

func (c *Client) Health() error {
	_, err := c.api.Sys().Health()
	return err
}

// CreateKey requests the kms-vault-plugin to generate a new secp256k1 key at
// the given path. Idempotent — plugin returns the existing key info if it
// already exists.
func (c *Client) CreateKey(ctx context.Context, path string) error {
	if err := ValidateKeyPath(path); err != nil {
		return err
	}
	_, err := c.api.Logical().WriteWithContext(ctx, ToVaultPath(path), map[string]any{})
	return mapVaultErr(path, err)
}

// GetPublicKey returns the 65-byte uncompressed secp256k1 public key for the
// given key path. The plugin returns a compressed (33-byte) key; we decompress
// here so downstream signers continue to receive the uncompressed form.
func (c *Client) GetPublicKey(ctx context.Context, path string) ([]byte, error) {
	if err := ValidateKeyPath(path); err != nil {
		return nil, err
	}
	secret, err := c.api.Logical().ReadWithContext(ctx, ToVaultPath(path))
	if err != nil {
		return nil, mapVaultErr(path, err)
	}
	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("%w: key not found: %s", types.ErrNotFound, path)
	}
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
	return crypto.FromECDSAPub(pub), nil
}

// Sign submits a pre-hashed 32-byte input to the kms-vault-plugin and returns
// the (r, s) components of the resulting low-S-normalised secp256k1 signature.
func (c *Client) Sign(ctx context.Context, path string, hash []byte) (r, s *big.Int, err error) {
	if err := ValidateKeyPath(path); err != nil {
		return nil, nil, err
	}
	if len(hash) != 32 {
		return nil, nil, errors.New("payload must be 32 bytes (pre-hashed)")
	}
	secret, err := c.api.Logical().WriteWithContext(ctx, ToSignPath(path), map[string]any{
		"input": hex.EncodeToString(hash),
	})
	if err != nil {
		return nil, nil, mapVaultErr(path, err)
	}
	if secret == nil || secret.Data == nil {
		return nil, nil, fmt.Errorf("%w: key not found during sign: %s", types.ErrNotFound, path)
	}
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

// ListKeys lists key names under the given prefix via the plugin's LIST endpoint.
func (c *Client) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	path := "kms/keys"
	if prefix != "" {
		path = "kms/keys/" + prefix
	}
	secret, err := c.api.Logical().ListWithContext(ctx, path)
	if err != nil {
		return nil, err
	}
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

func mapVaultErr(path string, err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "permission") || strings.Contains(msg, "denied") || strings.Contains(msg, "403"):
		return fmt.Errorf("%w: %s", types.ErrPermission, err)
	case strings.Contains(msg, "not found") || strings.Contains(msg, "404"):
		return fmt.Errorf("%w: key not found: %s", types.ErrNotFound, path)
	default:
		return err
	}
}

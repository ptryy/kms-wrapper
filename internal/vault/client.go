package vault

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

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

func (c *Client) CreateKey(ctx context.Context, path string) error {
	if err := ValidateKeyPath(path); err != nil {
		return err
	}
	_, err := c.api.Logical().WriteWithContext(ctx, ToVaultPath(path), map[string]any{"type": "ecdsa-p256k1"})
	return mapVaultErr(path, err)
}

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
	return publicKeyFromSecret(secret.Data)
}

func (c *Client) Sign(ctx context.Context, path string, hash []byte) (r, s *big.Int, err error) {
	if err := ValidateKeyPath(path); err != nil {
		return nil, nil, err
	}
	if len(hash) != 32 {
		return nil, nil, errors.New("payload must be 32 bytes (pre-hashed)")
	}
	secret, err := c.api.Logical().WriteWithContext(ctx, "transit/sign/"+path, map[string]any{
		"input":          base64.StdEncoding.EncodeToString(hash),
		"hash_algorithm": "none",
	})
	if err != nil {
		return nil, nil, mapVaultErr(path, err)
	}
	if secret == nil || secret.Data == nil {
		return nil, nil, fmt.Errorf("%w: key not found during sign: %s", types.ErrNotFound, path)
	}
	sig, ok := secret.Data["signature"].(string)
	if !ok || sig == "" {
		return nil, nil, errors.New("vault signature missing")
	}
	parts := strings.Split(sig, ":")
	der, err := base64.StdEncoding.DecodeString(parts[len(parts)-1])
	if err != nil {
		return nil, nil, fmt.Errorf("decode vault signature: %w", err)
	}
	var parsed struct {
		R, S *big.Int
	}
	if _, err := asn1.Unmarshal(der, &parsed); err != nil {
		return nil, nil, fmt.Errorf("decode DER signature: %w", err)
	}
	return parsed.R, parsed.S, nil
}

// ListKeys lists key names under the given prefix in Vault Transit.
func (c *Client) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	path := "transit/keys"
	if prefix != "" {
		path = "transit/keys/" + prefix
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

// publicKeyFromSecret returns the public key for the latest key version.
func publicKeyFromSecret(data map[string]any) ([]byte, error) {
	if keys, ok := data["keys"].(map[string]any); ok {
		var latestVer int
		var chosen string
		for k, v := range keys {
			ver, _ := strconv.Atoi(k)
			var pk string
			if m, ok := v.(map[string]any); ok {
				pk, _ = m["public_key"].(string)
			} else {
				pk, _ = v.(string)
			}
			if pk != "" && ver >= latestVer {
				latestVer = ver
				chosen = pk
			}
		}
		if chosen != "" {
			return parsePublicKey(chosen)
		}
	}
	if pk, ok := data["public_key"].(string); ok {
		return parsePublicKey(pk)
	}
	return nil, errors.New("vault public key missing")
}

func parsePublicKey(s string) ([]byte, error) {
	if block, _ := pem.Decode([]byte(s)); block != nil {
		if pub, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
			if ecdsaPub, ok := pub.(*ecdsa.PublicKey); ok {
				return elliptic.Marshal(ecdsaPub.Curve, ecdsaPub.X, ecdsaPub.Y), nil
			}
		}
		return publicKeyFromSPKI(block.Bytes)
	}
	if raw, err := hex.DecodeString(strings.TrimPrefix(s, "0x")); err == nil && len(raw) == 65 {
		return raw, nil
	}
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil {
		if len(raw) == 65 {
			return raw, nil
		}
		if out, err := publicKeyFromSPKI(raw); err == nil {
			return out, nil
		}
	}
	return nil, errors.New("unsupported vault public key encoding")
}

func publicKeyFromSPKI(der []byte) ([]byte, error) {
	var spki struct {
		Algorithm        asn1.RawValue
		SubjectPublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(der, &spki); err != nil {
		return nil, err
	}
	pub := spki.SubjectPublicKey.Bytes
	if len(pub) != 65 || pub[0] != 4 {
		return nil, errors.New("public key is not uncompressed secp256k1")
	}
	return pub, nil
}

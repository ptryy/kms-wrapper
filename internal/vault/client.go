package vault

import (
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
	"strings"

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
	if err := c.Health(); err != nil {
		return nil, fmt.Errorf("vault unreachable at %s: %w", addr, err)
	}
	return c, nil
}

func (c *Client) Health() error {
	_, err := c.api.Sys().Health()
	return err
}

func (c *Client) CreateKey(path string) error {
	if err := ValidateKeyPath(path); err != nil {
		return err
	}
	_, err := c.api.Logical().Write(ToVaultPath(path), map[string]any{"type": "ecdsa-p256k1"})
	return mapVaultErr(path, err)
}

func (c *Client) GetPublicKey(path string) ([]byte, error) {
	if err := ValidateKeyPath(path); err != nil {
		return nil, err
	}
	secret, err := c.api.Logical().Read(ToVaultPath(path))
	if err != nil {
		return nil, mapVaultErr(path, err)
	}
	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("%w: key not found: %s", types.ErrNotFound, path)
	}
	publicKey, err := publicKeyFromSecret(secret.Data)
	if err != nil {
		return nil, err
	}
	return publicKey, nil
}

func (c *Client) Sign(path string, hash []byte) (r, s *big.Int, err error) {
	if err := ValidateKeyPath(path); err != nil {
		return nil, nil, err
	}
	if len(hash) != 32 {
		return nil, nil, errors.New("payload must be 32 bytes (pre-hashed)")
	}
	secret, err := c.api.Logical().Write("transit/sign/"+path, map[string]any{
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

func publicKeyFromSecret(data map[string]any) ([]byte, error) {
	if keys, ok := data["keys"].(map[string]any); ok {
		var chosen string
		for _, v := range keys {
			if m, ok := v.(map[string]any); ok {
				if pk, ok := m["public_key"].(string); ok {
					chosen = pk
				}
			} else if pk, ok := v.(string); ok {
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

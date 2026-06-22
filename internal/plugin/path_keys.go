package kmsplugin

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"

	"github.com/ryan-truong/kms-wrapper/internal/keypath"
	"github.com/ryan-truong/kms-wrapper/pkg/types"
)

// keyStoragePrefix is the storage namespace under which KeyEntry records live.
const keyStoragePrefix = "keys/"

func (b *backend) pathsKeys() []*framework.Path {
	nameField := map[string]*framework.FieldSchema{
		"name": {
			Type:        framework.TypeString,
			Description: "Key name. Must be a hierarchical multi-tenant path of the form {project}/{environment}/{username} (segments match [a-z0-9_-]).",
		},
		"chains": {
			Type:        framework.TypeString,
			Description: "Comma-separated signing chains to persist with the key.",
		},
		"add_chains": {
			Type:        framework.TypeString,
			Description: "Comma-separated signing chains to add to the persisted allow-list.",
		},
	}
	return []*framework.Path{
		{
			Pattern: "keys/?$",
			Operations: map[logical.Operation]framework.OperationHandler{
				logical.ListOperation: &framework.PathOperation{Callback: b.handleListKeys},
			},
			HelpSynopsis: "List managed secp256k1 keys.",
		},
		{
			Pattern: "keys/(?P<name>.+?)/?$",
			Fields:  nameField,
			Operations: map[logical.Operation]framework.OperationHandler{
				logical.CreateOperation: &framework.PathOperation{Callback: b.handleCreateKey},
				logical.UpdateOperation: &framework.PathOperation{Callback: b.handleKeyWrite},
				logical.ReadOperation:   &framework.PathOperation{Callback: b.handleReadKey},
				logical.DeleteOperation: &framework.PathOperation{Callback: b.handleDeleteKey},
				logical.ListOperation:   &framework.PathOperation{Callback: b.handleListKeys},
			},
			ExistenceCheck: b.handleKeyExistence,
			HelpSynopsis:   "Manage a secp256k1 key (create, read, delete).",
		},
	}
}

func (b *backend) handleKeyWrite(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	if _, ok := data.GetOk("chains"); ok {
		return b.handleCreateKey(ctx, req, data)
	}
	return b.handleUpdateChains(ctx, req, data)
}

func (b *backend) keyName(req *logical.Request, data *framework.FieldData) string {
	if data != nil {
		if v, ok := data.GetOk("name"); ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	// Fall back to request path (strip "keys/" prefix).
	if len(req.Path) > len(keyStoragePrefix) && req.Path[:len(keyStoragePrefix)] == keyStoragePrefix {
		return req.Path[len(keyStoragePrefix):]
	}
	return ""
}

func (b *backend) handleKeyExistence(ctx context.Context, req *logical.Request, data *framework.FieldData) (bool, error) {
	name := b.keyName(req, data)
	if name == "" {
		return false, nil
	}
	entry, err := req.Storage.Get(ctx, keyStoragePrefix+name)
	if err != nil {
		return false, err
	}
	return entry != nil, nil
}

func (b *backend) handleCreateKey(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	name := b.keyName(req, data)
	if name == "" {
		return logical.ErrorResponse("key name is required"), nil
	}
	if err := keypath.Validate(name); err != nil {
		return logical.ErrorResponse("%s", err.Error()), logical.ErrInvalidRequest
	}

	// Hold the per-key lock for the whole create: the idempotent legacy-backfill
	// branch below does a load→store that must not interleave with a concurrent
	// update-chains (same lock) on the same key, and two concurrent creates of
	// the same new key must not both generate material.
	lock := b.keyLock(name)
	lock.Lock()
	defer lock.Unlock()

	chains, err := requestChains(data)
	if err != nil {
		return logical.ErrorResponse("%s", err.Error()), logical.ErrInvalidRequest
	}
	parsedChains, err := types.ParseChains(chains)
	if err != nil {
		return logical.ErrorResponse("%s", err.Error()), logical.ErrInvalidRequest
	}

	// Idempotent: if the key already exists, return its current info without regenerating.
	existing, err := b.loadKey(ctx, req, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if len(existing.Chains) == 0 {
			// Intentional capability grant, not a fail-closed hole: a legacy key
			// with no persisted chains still 403s on every sign until chains are
			// added — either via update-chains (PATCH, requires update authority)
			// or by re-creating the key with chains (idempotent create, requires
			// create authority). This repairs the key in place without weakening
			// the sign-time fail-closed guarantee.
			existing.Chains = append([]string(nil), chainsToStrings(parsedChains)...)
			if err := b.storeKey(ctx, req, name, existing); err != nil {
				return nil, err
			}
			return keyInfoResponse(existing), nil
		}
		if !sameChainSet(existing.Chains, chainsToStrings(parsedChains)) {
			return logical.ErrorResponse("chains mismatch on idempotent create"), logical.ErrInvalidRequest
		}
		return keyInfoResponse(existing), nil
	}

	priv, err := crypto.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("generate secp256k1 key: %w", err)
	}
	entry := &KeyEntry{
		PrivateKey:       crypto.FromECDSA(priv),
		CompressedPubKey: crypto.CompressPubkey(&priv.PublicKey),
		EVMAddress:       crypto.PubkeyToAddress(priv.PublicKey).Hex(),
		Chains:           chainsToStrings(parsedChains),
		Source:           "generated",
		CreatedAt:        time.Now().UTC(),
	}
	if err := b.storeKey(ctx, req, name, entry); err != nil {
		return nil, err
	}
	return keyInfoResponse(entry), nil
}

func (b *backend) handleReadKey(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	name := b.keyName(req, data)
	if name == "" {
		return logical.ErrorResponse("key name is required"), nil
	}
	entry, err := b.loadKey(ctx, req, name)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}
	return keyInfoResponse(entry), nil
}

func (b *backend) handleDeleteKey(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	name := b.keyName(req, data)
	if name == "" {
		return logical.ErrorResponse("key name is required"), nil
	}
	if err := req.Storage.Delete(ctx, keyStoragePrefix+name); err != nil {
		return nil, err
	}
	return nil, nil
}

func (b *backend) handleListKeys(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	prefix := keyStoragePrefix
	if data != nil {
		if v, ok := data.GetOk("name"); ok {
			if s, ok := v.(string); ok && s != "" {
				if err := keypath.ValidateListPrefix(s); err != nil {
					return logical.ErrorResponse("%s", err.Error()), logical.ErrInvalidRequest
				}
				prefix = keyStoragePrefix + s
				if prefix[len(prefix)-1] != '/' {
					prefix += "/"
				}
			}
		}
	}
	names, err := req.Storage.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	return logical.ListResponse(names), nil
}

func (b *backend) loadKey(ctx context.Context, req *logical.Request, name string) (*KeyEntry, error) {
	raw, err := req.Storage.Get(ctx, keyStoragePrefix+name)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	var entry KeyEntry
	if err := raw.DecodeJSON(&entry); err != nil {
		return nil, fmt.Errorf("decode key entry %q: %w", name, err)
	}
	return &entry, nil
}

func (b *backend) storeKey(ctx context.Context, req *logical.Request, name string, entry *KeyEntry) error {
	storageEntry, err := logical.StorageEntryJSON(keyStoragePrefix+name, entry)
	if err != nil {
		return err
	}
	return req.Storage.Put(ctx, storageEntry)
}

func requestChains(data *framework.FieldData) ([]string, error) {
	if data == nil {
		return nil, fmt.Errorf("chains is required and must be a non-empty subset of [evm, cosmos]")
	}
	raw, ok := data.GetOk("chains")
	if !ok {
		return nil, fmt.Errorf("chains is required and must be a non-empty subset of [evm, cosmos]")
	}
	switch v := raw.(type) {
	case string:
		return strings.Split(v, ","), nil
	case []string:
		return v, nil
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("chains is required and must be a non-empty subset of [evm, cosmos]")
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("chains is required and must be a non-empty subset of [evm, cosmos]")
	}
}

func chainsToStrings(chains []types.Chain) []string {
	out := make([]string, 0, len(chains))
	for _, c := range chains {
		out = append(out, string(c))
	}
	return out
}

func sameChainSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, chain := range a {
		counts[chain]++
	}
	for _, chain := range b {
		n, ok := counts[chain]
		if !ok {
			return false
		}
		if n == 1 {
			delete(counts, chain)
			continue
		}
		counts[chain] = n - 1
	}
	return len(counts) == 0
}

// keyInfoResponse returns the public-safe view of a KeyEntry — never the private key.
func keyInfoResponse(entry *KeyEntry) *logical.Response {
	// Serialize chains as a non-nil slice so legacy keys with no persisted
	// chains marshal to JSON [] (not null); a null would break the gateway/CLI
	// decodeStringSlice path with "missing chains field".
	chains := make([]string, 0, len(entry.Chains))
	chains = append(chains, entry.Chains...)
	data := map[string]interface{}{
		"compressed_pub_key": base64.StdEncoding.EncodeToString(entry.CompressedPubKey),
		"evm_address":        entry.EVMAddress,
		"chains":             chains,
		"source":             entry.Source,
		"created_at":         entry.CreatedAt.Format(time.RFC3339Nano),
	}
	if entry.ImportedAt != nil {
		data["imported_at"] = entry.ImportedAt.Format(time.RFC3339Nano)
	}
	return &logical.Response{Data: data}
}

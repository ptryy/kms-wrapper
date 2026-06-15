package kmsplugin

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"

	"github.com/ryan-truong/kms-wrapper/internal/keypath"
)

// keyStoragePrefix is the storage namespace under which KeyEntry records live.
const keyStoragePrefix = "keys/"

func (b *backend) pathsKeys() []*framework.Path {
	nameField := map[string]*framework.FieldSchema{
		"name": {
			Type:        framework.TypeString,
			Description: "Key name. May contain `/` to support hierarchical multi-tenant naming (project/chain/user).",
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
				logical.UpdateOperation: &framework.PathOperation{Callback: b.handleCreateKey},
				logical.ReadOperation:   &framework.PathOperation{Callback: b.handleReadKey},
				logical.DeleteOperation: &framework.PathOperation{Callback: b.handleDeleteKey},
				logical.ListOperation:   &framework.PathOperation{Callback: b.handleListKeys},
			},
			ExistenceCheck: b.handleKeyExistence,
			HelpSynopsis:   "Manage a secp256k1 key (create, read, delete).",
		},
	}
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

	// Idempotent: if the key already exists, return its current info without regenerating.
	existing, err := b.loadKey(ctx, req, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
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

// keyInfoResponse returns the public-safe view of a KeyEntry — never the private key.
func keyInfoResponse(entry *KeyEntry) *logical.Response {
	data := map[string]interface{}{
		"compressed_pub_key": base64.StdEncoding.EncodeToString(entry.CompressedPubKey),
		"evm_address":        entry.EVMAddress,
		"source":             entry.Source,
		"created_at":         entry.CreatedAt.Format(time.RFC3339Nano),
	}
	if entry.ImportedAt != nil {
		data["imported_at"] = entry.ImportedAt.Format(time.RFC3339Nano)
	}
	return &logical.Response{Data: data}
}

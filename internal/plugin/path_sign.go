package kmsplugin

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"

	"github.com/ryan-truong/kms-wrapper/internal/keypath"
)

func (b *backend) pathsSign() []*framework.Path {
	return []*framework.Path{
		{
			Pattern: "sign/(?P<name>.+?)$",
			Fields: map[string]*framework.FieldSchema{
				"name": {
					Type:        framework.TypeString,
					Description: "Key name to sign with.",
				},
				"chain": {
					Type:        framework.TypeString,
					Required:    true,
					Description: "Signing chain to authorize against the key's persisted chain allow-list.",
				},
				"input": {
					Type:        framework.TypeString,
					Description: "Hex-encoded 32-byte pre-hashed input (64 hex chars).",
				},
			},
			Operations: map[logical.Operation]framework.OperationHandler{
				logical.UpdateOperation: &framework.PathOperation{Callback: b.handleSign},
			},
			HelpSynopsis: "Sign a pre-hashed 32-byte input with the named secp256k1 key.",
		},
	}
}

func (b *backend) handleSign(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	name, _ := data.GetOk("name")
	nameStr, _ := name.(string)
	if nameStr == "" {
		return logical.ErrorResponse("key name is required"), nil
	}
	if err := keypath.Validate(nameStr); err != nil {
		return logical.ErrorResponse("%s", err.Error()), logical.ErrInvalidRequest
	}

	inputRaw, ok := data.GetOk("input")
	if !ok {
		return logical.ErrorResponse("input is required (hex-encoded 32-byte digest)"), nil
	}
	inputStr, ok := inputRaw.(string)
	if !ok || inputStr == "" {
		return logical.ErrorResponse("input must be a non-empty hex string"), nil
	}
	digest, err := hex.DecodeString(inputStr)
	if err != nil {
		return logical.ErrorResponse("input must be hex-encoded: %s", err.Error()), nil
	}
	if len(digest) != 32 {
		return logical.ErrorResponse("input must be exactly 32 bytes (got %d)", len(digest)), nil
	}

	entry, err := b.loadKey(ctx, req, nameStr)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return logical.ErrorResponse("key not found: %s", nameStr), nil
	}

	chainRaw, ok := data.GetOk("chain")
	if !ok {
		return logical.ErrorResponse("chain is required"), logical.ErrInvalidRequest
	}
	chain, _ := chainRaw.(string)
	if chain == "" {
		return logical.ErrorResponse("chain is required"), logical.ErrInvalidRequest
	}

	allowedChains := append([]string(nil), entry.Chains...)
	sort.Strings(allowedChains)
	if !containsChain(allowedChains, chain) {
		return logical.ErrorResponse(
			"key %s not authorized for %s signing (allowed chains: [%s])",
			nameStr,
			chain,
			strings.Join(allowedChains, " "),
		), logical.ErrPermissionDenied
	}

	priv, err := crypto.ToECDSA(entry.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decode stored private key: %w", err)
	}

	sig, err := crypto.Sign(digest, priv)
	if err != nil {
		return nil, fmt.Errorf("secp256k1 sign: %w", err)
	}
	if len(sig) != 65 {
		return nil, fmt.Errorf("unexpected signature length %d", len(sig))
	}

	return &logical.Response{
		Data: map[string]interface{}{
			"r": hex.EncodeToString(sig[0:32]),
			"s": hex.EncodeToString(sig[32:64]),
		},
	}, nil
}

func containsChain(chains []string, chain string) bool {
	for _, allowed := range chains {
		if allowed == chain {
			return true
		}
	}
	return false
}

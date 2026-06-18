package kmsplugin

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"

	"github.com/ryan-truong/kms-wrapper/internal/keypath"
	"github.com/ryan-truong/kms-wrapper/pkg/types"
)

func (b *backend) pathsKeysUpdate() []*framework.Path {
	return []*framework.Path{
		{
			Pattern: "keys/(?P<name>.+?)/chains/?$",
			Fields: map[string]*framework.FieldSchema{
				"name": {
					Type:        framework.TypeString,
					Description: "Key name to update.",
				},
				"add_chains": {
					Type:        framework.TypeString,
					Description: "Comma-separated signing chains to add to the persisted allow-list.",
				},
			},
			Operations: map[logical.Operation]framework.OperationHandler{
				logical.UpdateOperation: &framework.PathOperation{Callback: b.handleUpdateChains},
			},
			HelpSynopsis: "Expand a key's persisted signing-chain allow-list.",
		},
	}
}

func (b *backend) handleUpdateChains(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	if err := updateChainsOnlyAdditions(data); err != nil {
		return logical.ErrorResponse("only add_chains is supported"), logical.ErrInvalidRequest
	}

	name, _ := data.GetOk("name")
	nameStr, _ := name.(string)
	if nameStr == "" {
		return logical.ErrorResponse("key name is required"), nil
	}
	if err := keypath.Validate(nameStr); err != nil {
		return logical.ErrorResponse("%s", err.Error()), logical.ErrInvalidRequest
	}

	addChains, err := requestAddChains(data)
	if err != nil {
		return logical.ErrorResponse("%s", err.Error()), logical.ErrInvalidRequest
	}
	parsedAddChains, err := types.ParseChains(addChains)
	if err != nil {
		return logical.ErrorResponse("%s", err.Error()), logical.ErrInvalidRequest
	}

	lock := b.keyLock(nameStr)
	lock.Lock()
	defer lock.Unlock()

	existing, err := b.loadKey(ctx, req, nameStr)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return logical.ErrorResponse("key not found: %s", nameStr), nil
	}

	chains := append([]string(nil), existing.Chains...)
	chains = append(chains, chainsToStrings(parsedAddChains)...)
	merged, err := types.ParseChains(chains)
	if err != nil {
		return logical.ErrorResponse("%s", err.Error()), logical.ErrInvalidRequest
	}

	mergedStrings := chainsToStrings(merged)
	if sameChainSet(existing.Chains, mergedStrings) {
		existing.Chains = mergedStrings
		return updateChainsResponse(nameStr, existing.Chains), nil
	}

	existing.Chains = mergedStrings
	if err := b.storeKey(ctx, req, nameStr, existing); err != nil {
		return nil, err
	}
	return updateChainsResponse(nameStr, existing.Chains), nil
}

func updateChainsOnlyAdditions(data *framework.FieldData) error {
	if data == nil {
		return fmt.Errorf("only add_chains is supported")
	}
	for name := range data.Raw {
		if name != "name" && name != "add_chains" {
			return fmt.Errorf("only add_chains is supported")
		}
	}
	if _, ok := data.GetOk("add_chains"); !ok {
		return fmt.Errorf("only add_chains is supported")
	}
	return nil
}

func requestAddChains(data *framework.FieldData) ([]string, error) {
	if data == nil {
		return nil, fmt.Errorf("add_chains is required and must be a non-empty subset of [evm, cosmos]")
	}
	raw, ok := data.GetOk("add_chains")
	if !ok {
		return nil, fmt.Errorf("add_chains is required and must be a non-empty subset of [evm, cosmos]")
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
				return nil, fmt.Errorf("add_chains is required and must be a non-empty subset of [evm, cosmos]")
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("add_chains is required and must be a non-empty subset of [evm, cosmos]")
	}
}

func (b *backend) keyLock(name string) *sync.Mutex {
	value, _ := b.keyLocks.LoadOrStore(name, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func updateChainsResponse(name string, chains []string) *logical.Response {
	return &logical.Response{
		Data: map[string]interface{}{
			"path":   name,
			"chains": append([]string(nil), chains...),
		},
	}
}

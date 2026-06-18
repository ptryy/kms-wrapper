// Package keyinfo derives the public key, EVM address, and Cosmos bech32
// address for a given key path. Both the CLI (`kms-wrapper keys show`) and the
// REST gateway (`GET /keys/info`) call this so the two surfaces never drift.
package keyinfo

import (
	"context"
	"encoding/hex"

	cosmossigner "github.com/ryan-truong/kms-wrapper/internal/signer/cosmos"
	evmsigner "github.com/ryan-truong/kms-wrapper/internal/signer/evm"
	"github.com/ryan-truong/kms-wrapper/pkg/types"
)

const DefaultHRP = "cosmos"

type KeyStore interface {
	GetPublicKey(ctx context.Context, path string) ([]byte, error)
}

// For fetches the public key for path and returns the derived KeyInfo.
// An empty hrp defaults to DefaultHRP. Errors from the underlying store
// (notably types.ErrNotFound) are returned unchanged so callers can
// errors.Is-check them.
func For(ctx context.Context, store KeyStore, path, hrp string, chains []types.Chain) (types.KeyInfo, error) {
	if hrp == "" {
		hrp = DefaultHRP
	}
	pub, err := store.GetPublicKey(ctx, path)
	if err != nil {
		return types.KeyInfo{}, err
	}
	info := types.KeyInfo{
		Path:         path,
		PublicKeyHex: hex.EncodeToString(pub),
		Chains:       chains,
	}
	if types.ChainsContain(chains, types.ChainEVM) {
		evmAddr, err := evmsigner.DeriveEVMAddress(pub)
		if err != nil {
			return types.KeyInfo{}, err
		}
		info.EVMAddress = evmAddr
	}
	if types.ChainsContain(chains, types.ChainCosmos) {
		cosmosAddr, err := cosmossigner.DeriveCosmosAddress(pub, hrp)
		if err != nil {
			return types.KeyInfo{}, err
		}
		info.CosmosAddress = cosmosAddr
	}
	return info, nil
}

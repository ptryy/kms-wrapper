package keyinfo

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"

	cosmossigner "github.com/ryan-truong/kms-wrapper/internal/signer/cosmos"
	"github.com/ryan-truong/kms-wrapper/pkg/types"
)

type fakeStore struct {
	pub []byte
	err error
}

func (f fakeStore) GetPublicKey(_ context.Context, _ string) ([]byte, error) {
	return f.pub, f.err
}

func TestForReturnsDerivedAddresses(t *testing.T) {
	key, _ := crypto.GenerateKey()
	pub := crypto.FromECDSAPub(&key.PublicKey)
	expectedEVM := crypto.PubkeyToAddress(key.PublicKey).Hex()

	tests := []struct {
		name          string
		chains        []types.Chain
		wantEVM       bool
		wantCosmos    bool
		wantChains    []types.Chain
		wantCosmosHRP string
	}{
		{name: "evm", chains: []types.Chain{types.ChainEVM}, wantEVM: true, wantCosmos: false, wantChains: []types.Chain{types.ChainEVM}, wantCosmosHRP: DefaultHRP},
		{name: "cosmos", chains: []types.Chain{types.ChainCosmos}, wantEVM: false, wantCosmos: true, wantChains: []types.Chain{types.ChainCosmos}, wantCosmosHRP: DefaultHRP},
		{name: "both", chains: []types.Chain{types.ChainEVM, types.ChainCosmos}, wantEVM: true, wantCosmos: true, wantChains: []types.Chain{types.ChainEVM, types.ChainCosmos}, wantCosmosHRP: DefaultHRP},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info, err := For(context.Background(), fakeStore{pub: pub}, "proj-a/prod/alice", "", tc.chains)
			if err != nil {
				t.Fatalf("For err=%v", err)
			}
			if info.Path != "proj-a/prod/alice" {
				t.Fatalf("path=%q", info.Path)
			}
			if info.PublicKeyHex != hex.EncodeToString(pub) {
				t.Fatalf("public_key_hex mismatch")
			}
			if got := types.ChainsContain(info.Chains, types.ChainEVM); got != tc.wantEVM {
				t.Fatalf("evm chain presence got=%v want=%v", got, tc.wantEVM)
			}
			if got := types.ChainsContain(info.Chains, types.ChainCosmos); got != tc.wantCosmos {
				t.Fatalf("cosmos chain presence got=%v want=%v", got, tc.wantCosmos)
			}
			if !reflect.DeepEqual(info.Chains, tc.wantChains) {
				t.Fatalf("chains got=%v want=%v", info.Chains, tc.wantChains)
			}
			if tc.wantEVM && info.EVMAddress != expectedEVM {
				t.Fatalf("evm_address got=%s want=%s", info.EVMAddress, expectedEVM)
			}
			if !tc.wantEVM && info.EVMAddress != "" {
				t.Fatalf("evm_address got=%q want empty", info.EVMAddress)
			}
			if tc.wantCosmos {
				expectedCosmos, err := cosmossigner.DeriveCosmosAddress(pub, tc.wantCosmosHRP)
				if err != nil {
					t.Fatalf("derive cosmos addr: %v", err)
				}
				if info.CosmosAddress != expectedCosmos {
					t.Fatalf("cosmos_address got=%s want=%s", info.CosmosAddress, expectedCosmos)
				}
			} else if info.CosmosAddress != "" {
				t.Fatalf("cosmos_address got=%q want empty", info.CosmosAddress)
			}
		})
	}
}

func TestForRespectsHRP(t *testing.T) {
	key, _ := crypto.GenerateKey()
	pub := crypto.FromECDSAPub(&key.PublicKey)
	expected, err := cosmossigner.DeriveCosmosAddress(pub, "osmo")
	if err != nil {
		t.Fatalf("derive cosmos: %v", err)
	}
	info, err := For(context.Background(), fakeStore{pub: pub}, "proj/prod/bob", "osmo", []types.Chain{types.ChainCosmos})
	if err != nil {
		t.Fatalf("For err=%v", err)
	}
	if info.CosmosAddress != expected {
		t.Fatalf("cosmos_address got=%s want=%s", info.CosmosAddress, expected)
	}
}

func TestForPropagatesNotFound(t *testing.T) {
	wrapped := fmt.Errorf("%w: key not found: proj/prod/ghost", types.ErrNotFound)
	_, err := For(context.Background(), fakeStore{err: wrapped}, "proj/prod/ghost", "", []types.Chain{types.ChainEVM})
	if err == nil || !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

package keyinfo

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
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
	expectedCosmos, err := cosmossigner.DeriveCosmosAddress(pub, DefaultHRP)
	if err != nil {
		t.Fatalf("derive cosmos addr: %v", err)
	}

	info, err := For(context.Background(), fakeStore{pub: pub}, "proj-a/evm/alice", "")
	if err != nil {
		t.Fatalf("For err=%v", err)
	}
	if info.Path != "proj-a/evm/alice" {
		t.Fatalf("path=%q", info.Path)
	}
	if info.PublicKeyHex != hex.EncodeToString(pub) {
		t.Fatalf("public_key_hex mismatch")
	}
	if info.EVMAddress != expectedEVM {
		t.Fatalf("evm_address got=%s want=%s", info.EVMAddress, expectedEVM)
	}
	if info.CosmosAddress != expectedCosmos {
		t.Fatalf("cosmos_address got=%s want=%s", info.CosmosAddress, expectedCosmos)
	}
}

func TestForRespectsHRP(t *testing.T) {
	key, _ := crypto.GenerateKey()
	pub := crypto.FromECDSAPub(&key.PublicKey)
	expected, err := cosmossigner.DeriveCosmosAddress(pub, "osmo")
	if err != nil {
		t.Fatalf("derive cosmos: %v", err)
	}
	info, err := For(context.Background(), fakeStore{pub: pub}, "proj/cosmos/bob", "osmo")
	if err != nil {
		t.Fatalf("For err=%v", err)
	}
	if info.CosmosAddress != expected {
		t.Fatalf("cosmos_address got=%s want=%s", info.CosmosAddress, expected)
	}
}

func TestForPropagatesNotFound(t *testing.T) {
	wrapped := fmt.Errorf("%w: key not found: proj/evm/ghost", types.ErrNotFound)
	_, err := For(context.Background(), fakeStore{err: wrapped}, "proj/evm/ghost", "")
	if err == nil || !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

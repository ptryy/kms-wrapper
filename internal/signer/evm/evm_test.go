package evm

import (
	"bytes"
	"context"
	"math/big"
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type mockVault struct{ priv []byte }

func (m mockVault) GetPublicKey(_ context.Context, _ string) ([]byte, error) {
	key, _ := crypto.ToECDSA(m.priv)
	return crypto.FromECDSAPub(&key.PublicKey), nil
}
func (m mockVault) Sign(_ context.Context, _ string, hash []byte) (*big.Int, *big.Int, error) {
	key, _ := crypto.ToECDSA(m.priv)
	sig, err := crypto.Sign(hash, key)
	return new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:64]), err
}

func TestDeriveEVMAddressAndSigners(t *testing.T) {
	key, _ := crypto.GenerateKey()
	v := mockVault{priv: crypto.FromECDSA(key)}
	signer := New(v)
	pub, _ := v.GetPublicKey(context.Background(), "")
	addr, err := DeriveEVMAddress(pub)
	if err != nil || addr != crypto.PubkeyToAddress(key.PublicKey).Hex() {
		t.Fatalf("addr=%s err=%v", addr, err)
	}
	ctx := context.Background()
	sig, err := signer.SignPersonalMessage(ctx, "proj/prod/alice", []byte("hello"))
	if err != nil || len(sig) != 65 {
		t.Fatalf("personal sig len=%d err=%v", len(sig), err)
	}
	eipSig, err := signer.SignEIP712Digest(ctx, "proj/prod/alice", bytes.Repeat([]byte{1}, 32))
	if err != nil || len(eipSig) != 65 {
		t.Fatalf("eip712 sig len=%d err=%v", len(eipSig), err)
	}
	if _, err := signer.SignEIP712Digest(ctx, "proj/prod/alice", []byte{1}); err == nil || err.Error() != "EIP-712 digest must be 32 bytes" {
		t.Fatalf("unexpected digest error %v", err)
	}
}

func TestSignRawTx(t *testing.T) {
	key, _ := crypto.GenerateKey()
	v := mockVault{priv: crypto.FromECDSA(key)}
	signer := New(v)
	unsigned := ethtypes.NewTransaction(0, crypto.PubkeyToAddress(key.PublicKey), big.NewInt(1), 21000, big.NewInt(1_000_000_000), nil)
	raw, _ := unsigned.MarshalBinary()
	ctx := context.Background()
	signedRaw, err := signer.SignRawTx(ctx, "proj/prod/alice", big.NewInt(1), raw)
	if err != nil {
		t.Fatal(err)
	}
	var signed ethtypes.Transaction
	if err := signed.UnmarshalBinary(signedRaw); err != nil {
		t.Fatal(err)
	}
	if signed.ChainId().Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("chain id=%s", signed.ChainId())
	}
	if _, err := signer.SignRawTx(ctx, "proj/prod/alice", big.NewInt(1), []byte("bad")); err == nil || err.Error() != "invalid RLP encoding" {
		t.Fatalf("unexpected rlp err %v", err)
	}
}

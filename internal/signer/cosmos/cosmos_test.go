package cosmos

import (
	"context"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"google.golang.org/protobuf/encoding/protowire"
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

func TestCosmosDeriveAndSign(t *testing.T) {
	key, _ := crypto.GenerateKey()
	v := mockVault{priv: crypto.FromECDSA(key)}
	pub, _ := v.GetPublicKey(context.Background(), "")
	addr, err := DeriveCosmosAddress(pub, "mantra")
	if err != nil || len(addr) < 8 || addr[:7] != "mantra1" {
		t.Fatalf("addr=%s err=%v", addr, err)
	}
	if _, err := DeriveCosmosAddress(pub, ""); err == nil || err.Error() != "invalid bech32 HRP" {
		t.Fatalf("unexpected hrp err %v", err)
	}
	s := New(v)
	ctx := context.Background()
	compressed, err := s.ExportCompressedPubKey(ctx, "proj/mantra/alice")
	if err != nil || len(compressed) != 33 {
		t.Fatalf("compressed len=%d err=%v", len(compressed), err)
	}
	doc := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), []byte("body"))
	sig, pk, err := s.SignDirect(ctx, "proj/mantra/alice", doc)
	if err != nil || len(sig) != 64 || len(pk) != 33 {
		t.Fatalf("direct sig=%d pk=%d err=%v", len(sig), len(pk), err)
	}
	sig, pk, err = s.SignAmino(ctx, "proj/mantra/alice", []byte(`{"b":2, "a":1}`+"\n"))
	if err != nil || len(sig) != 64 || len(pk) != 33 {
		t.Fatalf("amino sig=%d pk=%d err=%v", len(sig), len(pk), err)
	}
	if _, _, err := s.SignDirect(ctx, "proj/mantra/alice", []byte("bad")); err == nil || err.Error() != "invalid SignDoc proto encoding" {
		t.Fatalf("unexpected proto err %v", err)
	}
	_ = sha256.Size
}

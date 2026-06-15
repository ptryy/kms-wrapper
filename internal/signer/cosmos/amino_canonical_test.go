package cosmos

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"

	apptypes "github.com/ryan-truong/kms-wrapper/pkg/types"
)

// fixedKeyVault produces a deterministic secp256k1 ECDSA signature so the
// canonical-bytes test compares hash-of-canonical-form, not signature noise.
type fixedKeyVault struct{ priv []byte }

func (v fixedKeyVault) GetPublicKey(_ context.Context, _ string) ([]byte, error) {
	k, _ := crypto.ToECDSA(v.priv)
	return crypto.FromECDSAPub(&k.PublicKey), nil
}
func (v fixedKeyVault) Sign(_ context.Context, _ string, h []byte) (*big.Int, *big.Int, error) {
	k, _ := crypto.ToECDSA(v.priv)
	s, err := crypto.Sign(h, k)
	return new(big.Int).SetBytes(s[:32]), new(big.Int).SetBytes(s[32:64]), err
}

// TestSignAminoCanonicalMatchesUnmarshalMarshalRoundtrip pins our canonicaliser
// to the documented contract: Unmarshal→Marshal round-trip through encoding/
// json. The cosmos-sdk reference implementation is byte-identical, so this
// test catches any drift from the chain's verify path without dragging in
// cosmos-sdk's full module graph.
func TestSignAminoCanonicalMatchesUnmarshalMarshalRoundtrip(t *testing.T) {
	input := []byte(`{"b": 2, "a": [3, 1, 2], "nested": {"y": 1, "x": 2}}`)
	got, err := canonicaliseJSON(input)
	if err != nil {
		t.Fatalf("canonicalise: %v", err)
	}
	var v any
	if err := json.Unmarshal(input, &v); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(v)
	if string(got) != string(want) {
		t.Fatalf("canonical mismatch:\n got=%s\nwant=%s", got, want)
	}

	// Sanity: the digest of the canonical bytes is what we'd hand to Vault.
	d := sha256.Sum256(got)
	if len(d) != 32 {
		t.Fatalf("digest length wrong")
	}
}

func TestSignAminoRejectsDuplicateKeys(t *testing.T) {
	key, _ := crypto.GenerateKey()
	s := New(fixedKeyVault{priv: crypto.FromECDSA(key)})
	cases := []struct {
		name  string
		input string
	}{
		{"top-level", `{"a":1,"a":2}`},
		{"nested", `{"outer":{"k":1,"k":2}}`},
		{"in-array", `[{"k":1,"k":2}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := s.SignAmino(context.Background(), "proj/cosmos/alice", []byte(tc.input))
			if err == nil {
				t.Fatal("expected duplicate-key error")
			}
			if !errors.Is(err, apptypes.ErrBadRequest) {
				t.Fatalf("expected error to wrap ErrBadRequest, got %v", err)
			}
		})
	}

	// Happy path still works.
	if _, _, err := s.SignAmino(context.Background(), "proj/cosmos/alice", []byte(`{"a":1,"b":2}`)); err != nil {
		t.Fatalf("clean input should sign: %v", err)
	}
}

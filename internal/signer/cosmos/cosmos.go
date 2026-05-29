package cosmos

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/big"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/ripemd160"
	"google.golang.org/protobuf/encoding/protowire"
)

type Vault interface {
	GetPublicKey(ctx context.Context, path string) ([]byte, error)
	Sign(ctx context.Context, path string, hash []byte) (r, s *big.Int, err error)
}

type Signer struct{ vault Vault }

func New(v Vault) *Signer { return &Signer{vault: v} }

// DeriveCosmosAddress derives a bech32 address from a 65-byte uncompressed secp256k1 public key.
func DeriveCosmosAddress(pubkey []byte, hrp string) (string, error) {
	if hrp == "" {
		return "", errors.New("invalid bech32 HRP")
	}
	compressed, err := compress(pubkey)
	if err != nil {
		return "", err
	}
	return deriveFromCompressed(compressed, hrp)
}

// DeriveCosmosAddressFromCompressed derives a bech32 address from a 33-byte compressed secp256k1 public key.
func DeriveCosmosAddressFromCompressed(compressed []byte, hrp string) (string, error) {
	if len(compressed) != 33 {
		return "", errors.New("public key must be 33-byte compressed secp256k1")
	}
	if hrp == "" {
		return "", errors.New("invalid bech32 HRP")
	}
	return deriveFromCompressed(compressed, hrp)
}

func deriveFromCompressed(compressed []byte, hrp string) (string, error) {
	sha := sha256.Sum256(compressed)
	h := ripemd160.New()
	_, _ = h.Write(sha[:])
	addr, err := bech32.ConvertAndEncode(hrp, h.Sum(nil))
	if err != nil {
		return "", errors.New("invalid bech32 HRP")
	}
	return addr, nil
}

func (s *Signer) ExportCompressedPubKey(ctx context.Context, keyPath string) ([]byte, error) {
	pub, err := s.vault.GetPublicKey(ctx, keyPath)
	if err != nil {
		return nil, err
	}
	return compress(pub)
}

func (s *Signer) SignDirect(ctx context.Context, keyPath string, signDocBytes []byte) ([]byte, []byte, error) {
	if !validProto(signDocBytes) {
		return nil, nil, errors.New("invalid SignDoc proto encoding")
	}
	return s.signHash(ctx, keyPath, sha256.Sum256(signDocBytes))
}

// SignAmino signs a Cosmos Amino JSON sign document.
// Numbers are decoded as json.Number to avoid float64 precision loss on large integer values
// (e.g. coin amounts, timestamps) during JSON round-trip canonicalization.
func (s *Signer) SignAmino(ctx context.Context, keyPath string, stdSignDocJSON []byte) ([]byte, []byte, error) {
	dec := json.NewDecoder(bytes.NewReader(stdSignDocJSON))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, nil, err
	}
	canonical, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, err
	}
	return s.signHash(ctx, keyPath, sha256.Sum256(canonical))
}

func (s *Signer) signHash(ctx context.Context, keyPath string, hash [32]byte) ([]byte, []byte, error) {
	r, ss, err := s.vault.Sign(ctx, keyPath, hash[:])
	if err != nil {
		return nil, nil, err
	}
	pub, err := s.ExportCompressedPubKey(ctx, keyPath)
	if err != nil {
		return nil, nil, err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	ss.FillBytes(sig[32:])
	return sig, pub, nil
}

func compress(pubkey []byte) ([]byte, error) {
	if len(pubkey) != 65 || pubkey[0] != 4 {
		return nil, errors.New("public key must be 65-byte uncompressed secp256k1")
	}
	pub, err := crypto.UnmarshalPubkey(pubkey)
	if err != nil {
		return nil, err
	}
	return crypto.CompressPubkey(pub), nil
}

func validProto(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for len(b) > 0 {
		_, _, n := protowire.ConsumeField(b)
		if n < 0 {
			return false
		}
		b = b[n:]
	}
	return true
}

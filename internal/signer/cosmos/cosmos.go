package cosmos

import (
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
	GetPublicKey(path string) ([]byte, error)
	Sign(path string, hash []byte) (r, s *big.Int, err error)
}

type Signer struct{ vault Vault }

func New(v Vault) *Signer { return &Signer{vault: v} }

func DeriveCosmosAddress(pubkey []byte, hrp string) (string, error) {
	if hrp == "" {
		return "", errors.New("invalid bech32 HRP")
	}
	compressed, err := compress(pubkey)
	if err != nil {
		return "", err
	}
	sha := sha256.Sum256(compressed)
	h := ripemd160.New()
	_, _ = h.Write(sha[:])
	addr, err := bech32.ConvertAndEncode(hrp, h.Sum(nil))
	if err != nil {
		return "", errors.New("invalid bech32 HRP")
	}
	return addr, nil
}

func (s *Signer) ExportCompressedPubKey(keyPath string) ([]byte, error) {
	pub, err := s.vault.GetPublicKey(keyPath)
	if err != nil {
		return nil, err
	}
	return compress(pub)
}

func (s *Signer) SignDirect(keyPath string, signDocBytes []byte) ([]byte, []byte, error) {
	if !validProto(signDocBytes) {
		return nil, nil, errors.New("invalid SignDoc proto encoding")
	}
	return s.signHash(keyPath, sha256.Sum256(signDocBytes))
}

func (s *Signer) SignAmino(keyPath string, stdSignDocJSON []byte) ([]byte, []byte, error) {
	var doc any
	if err := json.Unmarshal(stdSignDocJSON, &doc); err != nil {
		return nil, nil, err
	}
	canonical, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, err
	}
	return s.signHash(keyPath, sha256.Sum256(canonical))
}

func (s *Signer) signHash(keyPath string, hash [32]byte) ([]byte, []byte, error) {
	r, ss, err := s.vault.Sign(keyPath, hash[:])
	if err != nil {
		return nil, nil, err
	}
	pub, err := s.ExportCompressedPubKey(keyPath)
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

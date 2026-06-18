package evm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type Vault interface {
	GetPublicKey(ctx context.Context, path string) ([]byte, error)
	Sign(ctx context.Context, path string, hash []byte, chain string) (r, s *big.Int, err error)
}

type Signer struct {
	vault Vault
}

func New(v Vault) *Signer { return &Signer{vault: v} }

func DeriveEVMAddress(pubkey []byte) (string, error) {
	if len(pubkey) != 65 || pubkey[0] != 4 {
		return "", errors.New("public key must be 65-byte uncompressed secp256k1")
	}
	pub, err := crypto.UnmarshalPubkey(pubkey)
	if err != nil {
		return "", err
	}
	return crypto.PubkeyToAddress(*pub).Hex(), nil
}

func (s *Signer) SignRawTx(ctx context.Context, keyPath string, chainID *big.Int, rawTx []byte) ([]byte, error) {
	var tx ethtypes.Transaction
	if err := tx.UnmarshalBinary(rawTx); err != nil {
		return nil, errors.New("invalid RLP encoding")
	}
	if chainID == nil || chainID.Sign() <= 0 {
		return nil, errors.New("chain ID is required")
	}
	if tx.Protected() && tx.ChainId() != nil && tx.ChainId().Sign() > 0 && tx.ChainId().Cmp(chainID) != 0 {
		return nil, errors.New("chain ID mismatch")
	}
	signer := ethtypes.LatestSignerForChainID(chainID)
	hash := signer.Hash(&tx).Bytes()
	sig, err := s.signWithRecovery(ctx, keyPath, hash)
	if err != nil {
		return nil, err
	}
	signed, err := tx.WithSignature(signer, sig)
	if err != nil {
		return nil, err
	}
	return signed.MarshalBinary()
}

func (s *Signer) SignPersonalMessage(ctx context.Context, keyPath string, msg []byte) ([]byte, error) {
	prefix := []byte("\x19Ethereum Signed Message:\n" + strconv.Itoa(len(msg)))
	return s.signWithRecovery(ctx, keyPath, crypto.Keccak256(append(prefix, msg...)))
}

func (s *Signer) SignEIP712Digest(ctx context.Context, keyPath string, digest []byte) ([]byte, error) {
	if len(digest) != 32 {
		return nil, errors.New("EIP-712 digest must be 32 bytes")
	}
	return s.signWithRecovery(ctx, keyPath, digest)
}

func (s *Signer) signWithRecovery(ctx context.Context, keyPath string, hash []byte) ([]byte, error) {
	r, ss, err := s.vault.Sign(ctx, keyPath, hash, "evm")
	if err != nil {
		return nil, err
	}
	pub, err := s.vault.GetPublicKey(ctx, keyPath)
	if err != nil {
		return nil, err
	}
	sig := make([]byte, 65)
	r.FillBytes(sig[0:32])
	ss.FillBytes(sig[32:64])
	for recID := byte(0); recID < 2; recID++ {
		sig[64] = recID
		recovered, err := crypto.Ecrecover(hash, sig)
		if err == nil && bytes.Equal(recovered, pub) {
			return sig, nil
		}
	}
	return nil, fmt.Errorf("could not recover signature for public key")
}

// NormalizeEthereumV converts recovery ID 0/1 to Ethereum v=27/28 for eth_sign (personal messages).
// Do NOT apply this to EIP-712 digests — EIP-712 consumers expect raw v=0/1.
func NormalizeEthereumV(sig []byte) []byte {
	out := append([]byte(nil), sig...)
	if len(out) == 65 && out[64] < 27 {
		out[64] += 27
	}
	return out
}

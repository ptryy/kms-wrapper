package cosmos

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/ripemd160" //nolint:gosec,staticcheck // ripemd160 is mandated by the Cosmos address scheme (bech32 of RIPEMD160(SHA256(pubkey))); it is protocol-required, not a discretionary hash choice
	"google.golang.org/protobuf/encoding/protowire"

	apptypes "github.com/ryan-truong/kms-wrapper/pkg/types"
)

// canonicaliseJSON mirrors cosmos-sdk/types.SortJSON byte-for-byte: an
// Unmarshal-then-Marshal round-trip through encoding/json, which orders
// object keys alphabetically and strips insignificant whitespace. We inline
// this here rather than importing cosmos-sdk/types directly because the
// types package transitively pulls in cometbft and the entire chain
// machinery — far more than a sign-bytes helper needs. Keeping the routine
// byte-equivalent to cosmos-sdk's is what lets a chain re-derive the same
// hash on verify.
func canonicaliseJSON(in []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(in, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

type Vault interface {
	GetPublicKey(ctx context.Context, path string) ([]byte, error)
	Sign(ctx context.Context, path string, hash []byte, chain string) (r, s *big.Int, err error)
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
	h := ripemd160.New() //nolint:gosec // ripemd160 is mandated by the Cosmos address scheme; required for correct address derivation
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

// SignAmino signs a Cosmos Amino JSON sign document. The input is first
// scanned for duplicate JSON keys at any nesting depth — Go's stdlib does
// not flag these, and a chain that re-derives sign bytes via canonical
// JSON would resolve the duplicate differently than the parser the signer
// used. The signer then canonicalises the input via cosmos-sdk's own
// `types.SortJSON` (the same function the chain uses on verify), hashes
// with SHA-256, and signs.
func (s *Signer) SignAmino(ctx context.Context, keyPath string, stdSignDocJSON []byte) ([]byte, []byte, error) {
	if err := detectDuplicateJSONKeys(stdSignDocJSON); err != nil {
		return nil, nil, err
	}
	canonical, err := canonicaliseJSON(stdSignDocJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("canonicalise amino sign doc: %w", err)
	}
	return s.signHash(ctx, keyPath, sha256.Sum256(canonical))
}

// detectDuplicateJSONKeys walks the JSON token stream and reports the first
// duplicate-key collision at any object scope. Returns an error wrapping
// apptypes.ErrBadRequest so the gateway maps it to HTTP 400.
func detectDuplicateJSONKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return scanJSONValue(dec)
}

// scanJSONValue consumes one JSON value from dec, recursing into objects and
// arrays. For objects, it tracks the set of seen keys at the current scope
// and aborts on the first repeat.
func scanJSONValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return scanJSONObject(dec)
		case '[':
			return scanJSONArray(dec)
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", t)
		}
	default:
		// Scalar — no nesting to scan.
		return nil
	}
}

func scanJSONObject(dec *json.Decoder) error {
	seen := make(map[string]struct{})
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("JSON object key not a string: %T", keyTok)
		}
		if _, dup := seen[key]; dup {
			return fmt.Errorf("duplicate key in amino sign doc: %s: %w", key, apptypes.ErrBadRequest)
		}
		seen[key] = struct{}{}
		if err := scanJSONValue(dec); err != nil {
			return err
		}
	}
	// Consume the closing '}'.
	if _, err := dec.Token(); err != nil {
		return err
	}
	return nil
}

func scanJSONArray(dec *json.Decoder) error {
	for dec.More() {
		if err := scanJSONValue(dec); err != nil {
			return err
		}
	}
	// Consume the closing ']'.
	if _, err := dec.Token(); err != nil {
		return err
	}
	return nil
}

func (s *Signer) signHash(ctx context.Context, keyPath string, hash [32]byte) ([]byte, []byte, error) {
	r, ss, err := s.vault.Sign(ctx, keyPath, hash[:], "cosmos")
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

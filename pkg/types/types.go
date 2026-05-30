package types

import "errors"

var (
	ErrNotFound     = errors.New("not found")
	ErrPermission   = errors.New("permission denied")
	ErrInvalidInput = errors.New("invalid input")
)

type EVMSignRequest struct {
	KeyPath         string `json:"key_path"`
	ChainID         int64  `json:"chain_id,omitempty"`
	RawTx           string `json:"raw_tx,omitempty"`
	PersonalMessage string `json:"personal_message,omitempty"`
	EIP712Digest    string `json:"eip712_digest,omitempty"`
}

type CosmosSignRequest struct {
	KeyPath  string `json:"key_path"`
	HRP      string `json:"hrp"`
	SignMode string `json:"sign_mode"`
	SignDoc  string `json:"sign_doc"`
}

type SignatureParts struct {
	R string `json:"r"`
	S string `json:"s"`
	V uint64 `json:"v"`
}

type SignResponse struct {
	SignedTx      string          `json:"signed_tx,omitempty"`
	Signature     any             `json:"signature,omitempty"`
	PubKey        string          `json:"pub_key,omitempty"`
	Parts         *SignatureParts `json:"signature_parts,omitempty"`
	CosmosAddress string          `json:"cosmos_address,omitempty"`
}

type KeyInfo struct {
	Path          string `json:"path"`
	PublicKeyHex  string `json:"public_key_hex"`
	EVMAddress    string `json:"evm_address"`
	CosmosAddress string `json:"cosmos_address"`
}

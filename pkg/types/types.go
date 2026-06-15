package types

import "errors"

var (
	ErrNotFound     = errors.New("not found")
	ErrPermission   = errors.New("permission denied")
	ErrInvalidInput = errors.New("invalid input")
	ErrBadRequest   = errors.New("bad request")
)

type EVMSignRequest struct {
	// Type discriminates which payload field is consulted. Required.
	Type            string `json:"type" binding:"required" enums:"raw_tx,personal_message,eip712_digest"`
	KeyPath         string `json:"key_path"`
	ChainID         int64  `json:"chain_id,omitempty"`
	RawTx           string `json:"raw_tx,omitempty"`
	PersonalMessage string `json:"personal_message,omitempty"`
	EIP712Digest    string `json:"eip712_digest,omitempty"`
}

type EVMSignRawTxRequest struct {
	KeyPath string `json:"key_path" binding:"required" example:"proj-a/evm/alice"`
	ChainID int64  `json:"chain_id" binding:"required" minimum:"1" example:"1"`
	RawTx   string `json:"raw_tx" binding:"required" pattern:"^(0x)?[0-9a-fA-F]+$"`
}

type EVMSignPersonalMessageRequest struct {
	KeyPath         string `json:"key_path" binding:"required" example:"proj-a/evm/alice"`
	PersonalMessage string `json:"personal_message" binding:"required" pattern:"^(0x)?[0-9a-fA-F]+$"`
}

type EVMSignEIP712Request struct {
	KeyPath      string `json:"key_path" binding:"required" example:"proj-a/evm/alice"`
	EIP712Digest string `json:"eip712_digest" binding:"required" pattern:"^(0x)?[0-9a-fA-F]{64}$"`
}

type CosmosSignRequest struct {
	KeyPath  string `json:"key_path" binding:"required"`
	HRP      string `json:"hrp,omitempty"`
	SignMode string `json:"sign_mode" binding:"required" enums:"DIRECT,AMINO_JSON"`
	// SignDoc is base64 protobuf bytes when sign_mode=DIRECT and raw JSON when sign_mode=AMINO_JSON.
	SignDoc string `json:"sign_doc" binding:"required"`
}

type ErrorResponse struct {
	Error string `json:"error" binding:"required" example:"unauthorized"`
}

type SignatureParts struct {
	R string `json:"r"`
	S string `json:"s"`
	V uint64 `json:"v"`
}

// EVMSignRawTxResponse is returned for type=raw_tx requests.
type EVMSignRawTxResponse struct {
	SignedTx string         `json:"signed_tx"`
	Parts    SignatureParts `json:"signature_parts"`
}

// EVMSignPersonalResponse is returned for type=personal_message and
// type=eip712_digest requests.
type EVMSignPersonalResponse struct {
	Signature string `json:"signature" pattern:"^0x[0-9a-fA-F]{130}$"`
}

// SignResponse is the legacy union shape returned by Cosmos sign and (until
// the OpenAPI postprocess split is in place) the EVM signing path. The
// Signature field is intentionally a string here; the OpenAPI surface
// splits it into two typed response variants via EVMSignRawTxResponse and
// EVMSignPersonalResponse.
type SignResponse struct {
	SignedTx      string          `json:"signed_tx,omitempty"`
	Signature     string          `json:"signature,omitempty"`
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

type KeyCreateRequest struct {
	Path string `json:"path" binding:"required" example:"proj-a/evm/alice"`
}

type KeyCreateResponse struct {
	KeyInfo
	AlreadyExisted bool `json:"already_existed" example:"false"`
}

type KeyListResponse struct {
	Keys       []string `json:"keys" example:"evm/alice,cosmos/bob"`
	Count      int      `json:"count" example:"2"`
	NextCursor string   `json:"next_cursor"`
}

package kmsplugin

import (
	"context"
	"time"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
)

const backendHelp = `
The kms-vault-plugin secrets engine generates and stores secp256k1 private keys
inside Vault's encrypted logical storage and signs pre-hashed 32-byte inputs.

It is purpose-built for multi-chain signing: callers (EVM and Cosmos gateways)
hash the payload with the chain-appropriate algorithm (keccak256 / SHA-256) and
submit the digest to this plugin for raw secp256k1 ECDSA signing.

Private key material never leaves the Vault process boundary.
`

// KeyEntry is the on-disk representation of a managed secp256k1 key.
// PrivateKey is never returned in any API response.
type KeyEntry struct {
	PrivateKey       []byte     `json:"private_key"`
	CompressedPubKey []byte     `json:"compressed_pub_key"`
	EVMAddress       string     `json:"evm_address"`
	Chains           []string   `json:"chains"`
	Source           string     `json:"source"`
	CreatedAt        time.Time  `json:"created_at"`
	ImportedAt       *time.Time `json:"imported_at,omitempty"`
}

type backend struct {
	*framework.Backend
}

// Factory is the entry point invoked by Vault's plugin loader.
func Factory(ctx context.Context, conf *logical.BackendConfig) (logical.Backend, error) {
	b := newBackend()
	if err := b.Setup(ctx, conf); err != nil {
		return nil, err
	}
	return b, nil
}

func newBackend() *backend {
	b := &backend{}
	b.Backend = &framework.Backend{
		Help:        backendHelp,
		BackendType: logical.TypeLogical,
		PathsSpecial: &logical.Paths{
			SealWrapStorage: []string{"keys/"},
		},
		Paths: framework.PathAppend(
			b.pathsKeys(),
			b.pathsSign(),
		),
	}
	return b
}

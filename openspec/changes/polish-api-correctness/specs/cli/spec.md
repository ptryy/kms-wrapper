## MODIFIED Requirements

### Requirement: `kms-wrapper sign cosmos` — sign a Cosmos transaction from CLI
The CLI SHALL provide `kms-wrapper sign cosmos --path <key-path> --hrp <hrp> --mode <DIRECT|AMINO_JSON> --sign-doc <doc>` to sign a Cosmos transaction and print the base64 signature and public key to stdout. The `--sign-doc` encoding SHALL depend on `--mode`: in `DIRECT` mode, the value SHALL be a base64-encoded protobuf `SignDoc`; in `AMINO_JSON` mode, the value SHALL be the raw amino JSON document (NOT base64-wrapped) — the CLI passes the raw bytes to the signer's canonicalisation step. Signer errors SHALL be propagated to the outer error path and surfaced as a non-zero exit code with a descriptive `stderr` message; the CLI SHALL NOT print an "empty success" response when the underlying sign operation fails. Specifically, the `err` variable assigned by `SignDirect` or `SignAmino` SHALL live in the same scope as the post-switch check that produces the CLI's exit code.

#### Scenario: Successful Cosmos sign
- **WHEN** valid arguments and a valid sign doc are provided
- **THEN** the CLI prints signature (base64) and compressed public key (base64) to stdout and exits 0

#### Scenario: Signer returns an error
- **WHEN** `SignDirect` or `SignAmino` returns an error (e.g. Vault unreachable, invalid sign doc, key not found)
- **THEN** the CLI exits with a non-zero code, prints the error message to stderr (wrapped as `"sign cosmos: <message>"`), and prints NOTHING to stdout (no zero-byte "signature" placeholder)

#### Scenario: Invalid base64 sign doc (DIRECT mode only)
- **WHEN** `--mode=DIRECT` AND `--sign-doc` is not valid base64
- **THEN** the CLI exits non-zero with stderr `"decode sign-doc: <base64 error>"` and prints nothing to stdout

#### Scenario: AMINO_JSON mode takes raw JSON, not base64
- **WHEN** `--mode=AMINO_JSON` AND `--sign-doc` is a raw amino JSON string (e.g. `{"account_number":"1",...}`)
- **THEN** the CLI passes the raw bytes to `SignAmino` without base64 decoding; a value that *looks* like base64 but is intended as raw JSON SHALL NOT be interpreted as base64 (no decode step runs in this mode)

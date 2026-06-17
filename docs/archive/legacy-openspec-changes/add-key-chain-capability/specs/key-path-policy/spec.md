## ADDED Requirements
### Requirement: Plugin persists per-key chains capability tag
The Vault secrets plugin SHALL persist a `chains` capability tag on every managed key, alongside the secp256k1 key material in the plugin's `KeyEntry`. The tag SHALL be a sorted, lowercased, deduplicated subset of the closed set `{"evm", "cosmos"}` and SHALL be non-empty. The plugin SHALL canonicalize the tag (lowercase, dedupe, sort) before storage so that downstream comparisons are equality checks.

At create time, the plugin SHALL require the caller to supply `chains`. Missing, empty, or unrecognized values SHALL be rejected with `logical.ErrInvalidRequest` (HTTP 400) and the message `"chains is required and must be a non-empty subset of [evm, cosmos]"`. Idempotent re-create with a different `chains` value SHALL be rejected with the message `"chains mismatch on idempotent create"`; the existing tag SHALL NOT be overwritten by a create call.

#### Scenario: Create persists canonicalized chains
- **WHEN** a Vault client calls `vault write kms/keys/proj-a/prod/alice chains=cosmos,EVM,cosmos`
- **THEN** the plugin creates the key with `KeyEntry.Chains = ["cosmos", "evm"]` (lowercased, deduped, sorted)

#### Scenario: Create with empty chains is rejected
- **WHEN** a Vault client calls `vault write kms/keys/proj-a/prod/alice chains=`
- **THEN** the plugin returns HTTP 400 with `"chains is required and must be a non-empty subset of [evm, cosmos]"` and the key is NOT created

#### Scenario: Create with unknown chain is rejected
- **WHEN** a Vault client calls `vault write kms/keys/proj-a/prod/alice chains=evm,solana`
- **THEN** the plugin returns HTTP 400 with `"chains is required and must be a non-empty subset of [evm, cosmos]"` and the key is NOT created

#### Scenario: Idempotent re-create preserves existing chains
- **WHEN** a key exists with `Chains=[evm]` and a Vault client re-runs `create chains=evm`
- **THEN** the plugin returns 200 and the key's `Chains` remain `[evm]` unchanged

#### Scenario: Idempotent re-create with mismatched chains is rejected
- **WHEN** a key exists with `Chains=[evm]` and a Vault client calls `create chains=evm,cosmos`
- **THEN** the plugin returns HTTP 400 with `"chains mismatch on idempotent create"` and the existing `Chains` are NOT modified

---

### Requirement: Plugin enforces chains tag at sign time
The Vault secrets plugin SHALL accept a required `chain` parameter (string, `evm`|`cosmos`) on every sign call. The plugin SHALL load the target `KeyEntry`, and if `chain` is not a member of `KeyEntry.Chains`, the plugin SHALL reject the sign call with `logical.ErrPermission` (HTTP 403) and the message exactly `"key <path> not authorized for <chain> signing (allowed chains: [<sorted-list>])"`. The plugin SHALL NOT sign the payload; no signature material SHALL be returned in the error body.

A key whose persisted `Chains` field is missing or empty (legacy data created before this requirement existed) SHALL be treated as `Chains=[]` and SHALL fail closed — every sign call returns 403 until the tag is set via the `update-chains` operation.

#### Scenario: Allowed chain signs
- **WHEN** a Vault client signs against a key with `Chains=[evm, cosmos]` and `chain=cosmos`
- **THEN** the plugin returns the signature (subject to existing sign semantics)

#### Scenario: Disallowed chain returns 403
- **WHEN** a Vault client signs against a key with `Chains=[evm]` and `chain=cosmos`
- **THEN** the plugin returns HTTP 403 with `"key proj-a/prod/alice not authorized for cosmos signing (allowed chains: [evm])"` and no signature is produced

#### Scenario: Legacy entry fails closed
- **WHEN** a Vault client signs against a key that exists with no persisted `Chains` field
- **THEN** the plugin returns HTTP 403 with `"key proj-a/prod/alice not authorized for evm signing (allowed chains: [])"` regardless of the `chain` parameter

---

### Requirement: Plugin supports expand-only chains updates
The Vault secrets plugin SHALL expose an `update-chains` operation on the keys endpoint accepting a single field `add_chains` (non-empty subset of `{"evm","cosmos"}`). The plugin SHALL compute the union of the existing `Chains` and `add_chains`, canonicalize, and persist the result. The operation SHALL be idempotent — adding a chain already present is a no-op that returns the existing list. The plugin SHALL reject any payload containing fields other than `add_chains` (specifically `chains` or `remove_chains`) with `logical.ErrInvalidRequest` (HTTP 400) and the message `"only add_chains is supported"`.

#### Scenario: Expand from evm to evm+cosmos
- **WHEN** a key has `Chains=[evm]` and a Vault client calls `update-chains add_chains=cosmos`
- **THEN** the plugin returns 200 with `chains=[cosmos, evm]` (canonical sorted order) and the persisted `KeyEntry.Chains` is `["cosmos", "evm"]`

#### Scenario: Idempotent add is a no-op
- **WHEN** a key has `Chains=[evm, cosmos]` and a Vault client calls `update-chains add_chains=evm`
- **THEN** the plugin returns 200 with `chains=[cosmos, evm]` unchanged and the persisted entry is not rewritten

#### Scenario: Remove attempt is rejected
- **WHEN** a Vault client calls `update-chains remove_chains=cosmos` on any key
- **THEN** the plugin returns HTTP 400 with `"only add_chains is supported"` and the entry is unmodified

#### Scenario: Replace attempt is rejected
- **WHEN** a Vault client calls `update-chains chains=cosmos` on any key
- **THEN** the plugin returns HTTP 400 with `"only add_chains is supported"` and the entry is unmodified

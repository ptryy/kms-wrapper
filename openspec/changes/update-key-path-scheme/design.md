## Context
The KMS wrapper exposes a single `secp256k1` key per Vault Transit path. The signer layer (`internal/signer/evm`, `internal/signer/cosmos`) is what decides whether a given signing request produces an EVM signature, an EIP-712 signature, a Cosmos DIRECT signature, or an Amino JSON signature. The key path itself is opaque to the signer — both `/sign/evm` and `/sign/cosmos` accept any `key_path` and call into Vault Transit with `hash_algorithm=none`. Nothing today binds a path to a chain.

The historical `{project}/{chain}/{username}` convention dates back to early design discussions where each chain was expected to use a different curve (Ed25519 for some Cosmos chains, secp256k1 for EVM). The plugin landed on secp256k1-only, but the `{chain}` segment stayed in the spec and in operator examples. Now the segment is misleading: nothing prevents `payment/evm/bob` from signing a Cosmos transaction, and operators have asked why.

## Goals / Non-Goals
- Goals
  - Make the path scheme honest about what it scopes (a project-internal namespace, not a chain).
  - Keep the existing 3-segment / `[a-z0-9_-]` regex / Vault policy glob structure unchanged so policy templates only need a label swap.
  - Single source of truth for the validator — `internal/keypath.Validate` continues to be shared by gateway, CLI, and plugin.
- Non-Goals
  - Enforcing per-environment Vault policies (operators are still free to define their own taxonomy: `prod`, `tier-a`, `wallet`, anything matching `[a-z0-9_-]+`).
  - Migrating existing Vault keys. This is a clean break; the project does not yet have customers in production.
  - Splitting EVM and Cosmos into separate keys. One secp256k1 key serving both signers is intentional and remains.

## Decisions
- **Decision**: Use `{environment}` as the segment label rather than `{namespace}`, `{purpose}`, or `{tier}`.
  - Alternatives considered:
    - `{namespace}` — too generic; collides with Kubernetes vocabulary in our deployment context.
    - `{purpose}` — implies a key-class enumeration (hot/cold/wallet) we do not want to standardize.
    - `{tier}` — implies a security/sensitivity ranking the project does not enforce.
  - `{environment}` is the closest match to how MANTRA already partitions Vault tokens, policies, and CI pipelines.
- **Decision**: Free-form environment values, no reserved list, no warnings.
  - The previous reserved chain list (`evm`, `eth`, `mantra`, ...) added no enforcement value; it only emitted a warning log on unknowns. Removing it simplifies the spec and avoids hard-coding a project-specific taxonomy.
- **Decision**: Clean break, no dual-mode validator.
  - The project is pre-production. Adding a deprecation shim and warning log would add code paths to maintain for callers that do not exist yet.

## Risks / Trade-offs
- **Risk**: Local dev workflows that hard-code old paths break on next `make dev-up`. Mitigation: README + bootstrap script are updated in the same PR; `make scrub-env && make dev-down && make dev-up` rebuilds from scratch.
- **Risk**: External docs (blog posts, screenshots, embedded snippets) still show `proj-a/evm/alice`. Mitigation: clearly mark the change as **BREAKING** in proposal.md and in the changelog/PR description.
- **Trade-off**: Free-form middle segment loses the documentation value of a reserved list. Acceptable because the list never enforced anything — it was a comment in spec form.

## Migration Plan
1. Land code, spec, and doc changes together (single PR).
2. Local dev: developers run `make scrub-env && make dev-down && make dev-up`. The bootstrap script regenerates a fresh Vault dev container; previously created keys are dropped.
3. Vault policies: anyone with a custom `policy-project.hcl` updates `kms/keys/<project>/<env>/*` and `kms/sign/<project>/<env>/*` globs (only the label changes; the glob structure is unchanged).
4. Client code: anyone calling `/keys`, `/keys/info`, `/sign/evm`, `/sign/cosmos`, or the CLI `keys create` updates the `key_path` argument to the new shape.
5. No data migration tooling is provided. Old keys remain in Vault until manually deleted; they are simply unreachable from the gateway.

## Open Questions
- None. Scope and direction confirmed in the conversation that produced this proposal.

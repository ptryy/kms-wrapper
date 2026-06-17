# Design: refactor-swagger-schema-names

**Date:** 2026-06-17
**Status:** Approved (migrated from legacy OpenSpec change `refactor-swagger-schema-names`, plus a regression guard)
**Governing doc:** [`/CONSTITUTION.md`](../../../CONSTITUTION.md)
**Breaking:** No (docs-only; downstream consumers regenerate clients against shorter names)

## Goal

Rewrite every OpenAPI schema component prefix from `github_com_ryan-truong_kms-wrapper_pkg_types.`
to `kms-wrapper_pkg_types.` in the `cmd/swagger-postprocess` pass, and fix the currently-broken EVM
discriminator mapping in the same pass. No `pkg/types` Go rename, no runtime change, no new dependency.

## Why

- The generated spec keys every schema under the fully-qualified Go import path, dominating every
  schema name and leaking the exact GitHub org+repo into downstream generated clients.
- The EVM discriminator mapping at `cmd/swagger-postprocess/main.go:117-119` references
  `#/components/schemas/types.EVMSignRawTxRequest` — but the generated keys are
  `github_com_ryan-truong_kms-wrapper_pkg_types.*`. **The mapping is dangling today**, so codegen does
  not produce the typed sealed-class hierarchy the `api-docs` spec requires. (Confirmed by reading
  `main.go` and the 45× long-prefix occurrences in `docs/`.)

Renaming the prefix decouples public schema names from the repo path and unbreaks the discriminator
in one pass.

## Scope

### Behavioral change (the postprocess tool)
- `cmd/swagger-postprocess/main.go`:
  - New `renameSchemaPrefix(spec map[string]any, oldPrefix, newPrefix string)` that:
    1. renames top-level keys under `components.schemas`;
    2. rewrites **every** `$ref` value containing `#/components/schemas/<oldPrefix>` — including those
       nested in `oneOf`/`allOf`/`anyOf`, `discriminator.mapping`, request bodies, response schemas,
       and nested property schemas.
  - Called from `normalizeSpec` **before** `injectEVMDiscriminator`.
  - `injectEVMDiscriminator` mapping values updated to
    `#/components/schemas/kms-wrapper_pkg_types.EVMSign{RawTx,PersonalMessage,EIP712}Request` (keeping
    the `#/components/schemas/` ref prefix).
- Regenerate `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml` via `make swagger`.

### Stability improvement (regression guard) — beyond legacy
Harden the `swagger-check` surface into a guard that fails CI when either:
1. the long repo-path prefix `github_com_ryan-truong_kms-wrapper_pkg_types` reappears in
   `docs/swagger.json` or `docs/docs.go`; **or**
2. any `discriminator.mapping` `$ref` is **dangling** — does not resolve to an existing key under
   `components.schemas`.

Guard (2) is what would have caught the current breakage; it prevents future `swag` output drift from
silently re-breaking codegen. Implemented as a `validateSpecRefs(spec)` step (run in the tool and/or a
`swagger-check` assertion).

## Out of scope
- No change to `pkg/types/types.go` (no Go-side rename).
- No change to handler signatures, Vault, or the CLI.

## Error handling

`renameSchemaPrefix` is a pure deterministic transform over the decoded spec map. If
`components.schemas` is absent it is a no-op. `validateSpecRefs` returns a descriptive error naming
the first dangling mapping ref so `make swagger-check` failure points the developer at the exact key.

## Testing

- `cmd/swagger-postprocess/main_test.go`:
  - fixture with an old-prefix schema key + a `$ref` to it (incl. inside `discriminator.mapping`) →
    assert both rewritten to the short prefix after `normalizeSpec`;
  - assert `injectEVMDiscriminator` produces mapping values pointing at the renamed schemas;
  - assert `validateSpecRefs` rejects a spec whose mapping `$ref` points at a missing key.
- `go test ./cmd/swagger-postprocess/...` and `go test ./...` green (confirms `pkg/types`/handlers
  untouched).
- After `make swagger`: `! grep -q github_com_ryan-truong_kms-wrapper_pkg_types docs/swagger.json`
  and every `discriminator.mapping` value resolves.
- `make swagger-check` clean; spec validates as OpenAPI 3.0.x.

## Risk

Very low — isolated to the postprocess tool and generated docs. Downstream consumers regenerate
clients; the only visible change is shorter, repo-independent schema names.

## Constitution alignment

- Docs remain a generated artifact, enforced by `swagger-check` (§4.7) — the regression guard
  strengthens that invariant.
- No impact on the security boundary, signing model, or key lifecycle.

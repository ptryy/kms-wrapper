## Context

The KMS wrapper exposes a small but tightly-scoped REST surface (`/health`, `/sign/evm`, `/sign/cosmos`) wired up in `internal/gateway/gateway.go`. Request and response types live in `pkg/types/types.go`. Today consumers must read Go source to learn:

- Which fields are required vs optional on `/sign/evm`.
- That exactly one of `raw_tx` / `personal_message` / `eip712_digest` must be provided, and that `raw_tx` additionally requires `chain_id`.
- That `eip712_digest` must be exactly 32 bytes hex.
- That `/sign/cosmos` accepts only `DIRECT` or `AMINO_JSON` for `sign_mode`, and that `sign_doc` is base64 for DIRECT but a raw JSON string for AMINO.

We want a stable, machine-readable contract document (OpenAPI 3.0) plus a try-it-out UI for operators. Generated docs must stay in sync with the code so the spec never lies.

**Stakeholders:** MANTRA Finance signer-team consumers, on-call operators debugging signing failures, and future SDK/codegen efforts.

**Constraints:**
- Pure-Go binary — no JS build step at deploy time.
- The binary must build with `go build ./...` even when `swag` is not installed locally.
- No new long-running processes; UI must be served by the existing `http.Server` in `internal/gateway`.
- Production deployments are typically behind an internal network but should be able to lock the docs surface down.

## Goals / Non-Goals

**Goals:**
- Publish an OpenAPI 3.0 spec at `GET /swagger/doc.json` describing every public endpoint, including precise validation rules (oneOf for the EVM payload union, enums for `sign_mode`, format hints like `byte` / `hex`).
- Serve Swagger UI at `GET /swagger/index.html` for interactive exploration, including bearer-token authorization for try-it-out flows.
- Make doc generation reproducible: one Makefile command (`make swagger`) regenerates the artifacts; a `swagger-check` target catches drift in CI.
- Allow operators to disable the docs surface entirely (`swagger_enabled=false`) or require auth on it (`swagger_auth=true`) via config — both togglable through env vars under the existing `KMS_GATEWAY_*` prefix.

**Non-Goals:**
- No client SDK generation in this change (the spec is the contract; codegen is a downstream concern).
- No versioning of the API surface — the spec describes the current `v0` shape; URI versioning is a separate proposal if/when needed.
- No automatic publication to an external docs portal (Redocly, SwaggerHub, etc.). The spec is served from the gateway itself.

## Decisions

### Decision: Use swaggo/swag for spec generation, not hand-written YAML

**Choice:** Annotate handlers with swaggo declarative comments; run `swag init -g cmd/kms-wrapper/root.go --output docs --outputTypes go,json,yaml --v3.1` (or v3.0 equivalent).

**Why:** Annotations live next to the code they describe, so PRs that touch handlers either update annotations or fail `make swagger-check`. Hand-written YAML drifts silently and was rejected on that basis. go-swagger was rejected because we don't need server/client codegen — only documentation.

**Trade-off:** Annotations are verbose and Go-comment-syntax is brittle (typos parse silently and emit a malformed spec). Mitigation: CI runs `make swagger-check` and gates on `git diff --exit-code` against the committed `docs/`.

### Decision: Target OpenAPI 3.0 via swaggo v2 (swaggo/swag v2.x)

**Choice:** Pin `swaggo/swag` at a v2.x release that emits OpenAPI 3.0. Swaggo's older v1 line emits Swagger 2.0, which cannot express `oneOf` or `discriminator`.

**Why:** We need `oneOf` to describe the EVM payload union accurately. Swagger 2.0 would force a single flat schema with a prose disclaimer, which is exactly the drift we are trying to eliminate.

**Trade-off:** swaggo v2 is younger than v1; some annotation syntaxes differ. If we hit a v2 bug, the fallback is hand-authoring a small `docs/swagger.json` patch in code — but we should not need to.

### Decision: Mount under `/swagger/*` using `swaggo/http-swagger`

**Choice:** Register two routes on the existing `http.ServeMux` in `internal/gateway/gateway.go`:

```go
mux.Handle("GET /swagger/", httpSwagger.Handler(
    httpSwagger.URL("/swagger/doc.json"),
))
```

This automatically exposes `/swagger/index.html`, `/swagger/doc.json`, and the bundled static assets.

**Why:** Default convention for Go projects using swaggo. `/docs` was considered but rejected to avoid collision with the existing `docs/` directory in the repo (mental-model confusion only — they're distinct surfaces — but `/swagger` is unambiguous).

**Trade-off:** Path is less friendly to non-Go consumers. If that becomes a complaint, we can add `gateway.swagger_base_path` as a follow-up; not worth shipping configurable in v1.

### Decision: Config-gated, not always-on

**Choice:** Add two fields to `config.Config.Gateway`:

```go
SwaggerEnabled bool `mapstructure:"swagger_enabled"` // default true
SwaggerAuth    bool `mapstructure:"swagger_auth"`    // default false
```

Bound to env vars `KMS_GATEWAY_SWAGGER_ENABLED` and `KMS_GATEWAY_SWAGGER_AUTH` through the existing viper flow.

- `swagger_enabled=false`: the `/swagger/*` routes are never registered. Hardening lever for internet-exposed deployments.
- `swagger_auth=true`: the existing `auth` middleware is applied to `/swagger/*`. Use when the gateway is behind a VPN/jump host but you still don't want the surface enumerable without a token.

**Why:** The user explicitly chose configurable (default public). This keeps developer ergonomics (zero-config docs in dev) while letting production raise the bar.

**Trade-off:** Two booleans is a touch more config surface than one. A single tri-state enum (`off | public | authed`) was considered but rejected — two independent booleans map more cleanly to env vars and to the typical "first I want to disable, then I want to lock down" deployment journey.

### Decision: Commit `docs/` to the repo, enforced by `swagger-check`

**Choice:** `make swagger` regenerates `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`. All three are checked in. CI step:

```make
swagger-check: swagger
	@git diff --exit-code docs/ || (echo 'swagger docs out of date — run make swagger' && exit 1)
```

**Why:** The binary must build without `swag` installed (clean `go build`), so the generated Go file must exist on disk. Committing the JSON/YAML lets external consumers fetch them straight from GitHub without running the gateway. The CI check is what keeps annotations honest.

**Trade-off:** Slightly noisier diffs on handler PRs (one logical change touches `gateway.go` + `docs/*`). Acceptable cost for an enforced source of truth.

### Decision: Model the EVM payload union with `oneOf` + three variant schemas

**Choice:** Define three separate request schemas in annotations (`EVMSignRawTxRequest`, `EVMSignPersonalMessageRequest`, `EVMSignEIP712Request`), each with the correct required fields, and have `/sign/evm` declare a request body of `oneOf: [...]` over them.

Constraints encoded in the schemas:
- `EVMSignRawTxRequest`: `key_path`, `chain_id` (int64, exclusiveMinimum 0), `raw_tx` (hex string) — all required.
- `EVMSignPersonalMessageRequest`: `key_path`, `personal_message` (hex string) — both required.
- `EVMSignEIP712Request`: `key_path`, `eip712_digest` (hex string, length 66 incl. 0x prefix, `pattern: ^(0x)?[0-9a-fA-F]{64}$`) — both required.

The Go struct `apptypes.EVMSignRequest` does NOT change — it stays a flat struct that the JSON decoder fills. The schema split lives only in annotations / generated docs.

**Why:** This is the only way the spec accurately documents the validation that `signEVM` already performs (`countNonEmpty == 1`, `chain_id > 0` when `raw_tx` set, 32-byte digest length). The "flat schema with optional fields + prose" alternative was rejected because it leaves consumers no way to know that the validator will reject ambiguous bodies.

**Trade-off:** Three schemas + a `oneOf` wrapper is more annotation overhead and slightly harder for downstream codegen to consume. Acceptable — accuracy wins.

### Decision: Generated `docs` package is imported for side-effects from `cmd/kms-wrapper/root.go`

**Choice:** Add `_ "github.com/ryan-truong/kms-wrapper/docs"` to the main command's imports so the generated `init()` registers the spec with `swag.Register`. `http-swagger` reads from `swag.Register` to serve `doc.json`.

**Why:** This is the swaggo idiom. Avoids needing to ship the JSON file as an `embed.FS` asset manually.

**Trade-off:** `docs/docs.go` is generated code in the import graph. Mitigated by the `swagger-check` CI gate and a top-of-file `// Code generated by swaggo/swag DO NOT EDIT.` comment.

## Risks / Trade-offs

- **[Risk] Annotation typos produce a malformed spec without failing the build.** swaggo will happily emit a half-empty schema if a field name is misspelled. → **Mitigation:** `make swagger-check` in CI plus a smoke test in `gateway_test.go` that fetches `/swagger/doc.json`, unmarshals it as OpenAPI 3.0, and asserts that the three signing operations are present with the expected response codes.

- **[Risk] Swagger UI in production exposes API surface to attackers.** Even with `swagger_auth=true`, a leaked token lets an attacker enumerate every endpoint. → **Mitigation:** Document the `swagger_enabled=false` posture as the default for internet-exposed deployments in `README.md`. Keep production deployments behind a VPN.

- **[Risk] swaggo/swag v2 is younger than v1 and may have bugs around `oneOf` / discriminators.** → **Mitigation:** Pin a specific minor version in `go.mod`; if a bug blocks accurate union schemas, fall back to hand-editing a small post-processing patch in the `make swagger` target (documented in the Makefile).

- **[Trade-off] Two extra deps (`swaggo/swag` CLI for build, `swaggo/http-swagger` for runtime).** http-swagger pulls in the Swagger UI static assets (~few hundred KB). Binary size grows modestly. Acceptable for an internal-facing service.

- **[Trade-off] `docs/` diff noise on handler PRs.** Reviewers must skip past regenerated JSON. Net positive — the diff proves docs were regenerated.

## Migration Plan

This is additive — no existing API contracts change.

1. Land config additions with default `swagger_enabled=true, swagger_auth=false` (current dev/internal posture is unchanged).
2. For production deployments, update Helm values / env templates to set `KMS_GATEWAY_SWAGGER_AUTH=true` (or `KMS_GATEWAY_SWAGGER_ENABLED=false` if the docs surface is unwanted) before rolling out.
3. Rollback is trivial: revert the gateway binary; no schema or data changes.

## Open Questions

- Should the OpenAPI `servers` block hard-code a URL, or be left empty so Swagger UI picks up the current origin? → Leaning empty (origin-relative), but confirm during implementation.
- Do we need a `/swagger/doc.yaml` alongside `doc.json`? swaggo emits both, but http-swagger by default only serves JSON. If anyone asks for YAML at runtime we can add a 5-line handler.
- Should `swagger_auth=true` reject Browser navigation to `/swagger/index.html` cleanly (HTML 401 page) or just send the JSON `{"error":"unauthorized"}`? The current auth middleware sends JSON; consistent behavior is probably fine.

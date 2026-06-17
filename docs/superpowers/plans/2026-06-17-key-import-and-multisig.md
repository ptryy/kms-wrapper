# key-import-and-multisig Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add key import (EVM raw key + Cosmos mnemonic), Cosmos multisig partial-sign, and EVM Gnosis Safe sign — as `/v1/` routes + CLI subcommands — with secrets held only in a bounded request memory window, zeroized, and never logged.

**Architecture:** Bottom-up. Plugin `import` handler is the trust boundary (validates + persists `source`/`imported_at`/`chains`). The Vault client adds `ImportKey`/`GetKeyInfo`; an internal derivation unit does BIP39/BIP44; gateway adds three dual-mounted routes that reuse #2's `authorizeChain` for the two sign routes; CLI adds three subcommands. Paths are `{project}/{environment}/{username}` (post-#1); imported keys carry the #2 `chains` tag.

**Tech Stack:** Go 1.25+, Vault plugin SDK, `github.com/tyler-smith/go-bip39`, `github.com/btcsuite/btcd/btcutil/hdkeychain`, `swaggo/swag` v2, Prometheus.

## Global Constraints

- **Depends on #1 + #2 landed.** Use `{environment}` paths (`payment/prod/alice`); imported keys persist a #2 `chains` tag; the two sign routes call `authorizeChain` (HTTP 403 on mismatch).
- Import requires `--chains` (no silent default). `--chain evm|cosmos` selects derivation only.
- Default Cosmos derivation path: `m/44'/118'/0'/0/0` (coin type 118).
- Secrets (`private_key_hex`, mnemonic, derived bytes) zeroized via `defer`; never logged; held only for the request.
- Import is non-idempotent: duplicate path → typed `types.ErrKeyExists` (from HTTP 409 status, never substring-matched) → HTTP 409. First success → HTTP 201.
- `safe_tx_hash` exactly 32 bytes; `signer_index` in range AND gateway pubkey == `multisig_pubkeys[signer_index]`.
- New routes dual-mounted (`/v1/...` primary; bare path `Deprecation`/`Sunset`), bearer-auth, shared rate limiter.
- Import authz at the Vault policy boundary (`create` on `kms/keys/+/import`); no second bearer token.
- Plugin-reality terms (`kms/...`), never Transit.

---

### Task 1: Plugin `import` handler + metadata

**Files:**
- Modify: `internal/plugin/path_keys.go` (new `import` operation), `internal/plugin/backend.go` (`KeyEntry`: ensure `Source`, `ImportedAt`, `Chains`)
- Test: `internal/plugin/path_keys_import_test.go` (new)

**Interfaces:**
- Produces: `POST kms/keys/<path>/import` accepting `private_key_hex` (+ `chains`); validates path (shared validator) then 32-byte secp256k1 scalar; persists `KeyEntry{Source:"imported", ImportedAt:now, Chains:<canonical>}`; duplicate → `logical.CodedError(409, "key already exists at path <path>")`; bad hex/length → 400 `private key must be 64 hex characters (32 bytes)`; bad scalar → 400 `invalid secp256k1 private key`. `GET kms/keys/<path>` returns `source`, `created_at`, `imported_at`.

- [ ] **Step 1: Write the failing tests**

Create `internal/plugin/path_keys_import_test.go`:

```go
func TestImport_SuccessPersistsMetadataAndChains(t *testing.T) {
	b, storage := newTestBackend(t)
	resp := importKey(t, b, storage, "payment/prod/alice", map[string]interface{}{
		"private_key_hex": validSecp256k1Hex, "chains": "evm",
	})
	if resp.Data["source"] != "imported" {
		t.Fatalf("source = %v, want imported", resp.Data["source"])
	}
	entry := readEntry(t, storage, "payment/prod/alice")
	if !reflect.DeepEqual(entry.Chains, []string{"evm"}) || entry.Source != "imported" || entry.ImportedAt == nil {
		t.Fatalf("entry not persisted correctly: %+v", entry)
	}
}

func TestImport_DuplicateReturns409(t *testing.T) {
	b, storage := newTestBackend(t)
	importKey(t, b, storage, "payment/prod/alice", map[string]interface{}{"private_key_hex": validSecp256k1Hex, "chains": "evm"})
	_, err := importKeyErr(b, storage, "payment/prod/alice", map[string]interface{}{"private_key_hex": validSecp256k1Hex, "chains": "evm"})
	var coded logical.HTTPCodedError
	if !errors.As(err, &coded) || coded.Code() != 409 {
		t.Fatalf("want 409 coded error, got %v", err)
	}
}

func TestImport_BadHexAndBadScalar(t *testing.T) {
	b, storage := newTestBackend(t)
	if _, err := importKeyErr(b, storage, "payment/prod/alice", map[string]interface{}{"private_key_hex": "zz", "chains": "evm"}); err == nil ||
		!strings.Contains(err.Error(), "64 hex characters") {
		t.Fatalf("want hex-length error, got %v", err)
	}
	if _, err := importKeyErr(b, storage, "payment/prod/alice", map[string]interface{}{"private_key_hex": zeroScalarHex, "chains": "evm"}); err == nil ||
		!strings.Contains(err.Error(), "invalid secp256k1 private key") {
		t.Fatalf("want scalar error, got %v", err)
	}
}
```

(`validSecp256k1Hex`, `zeroScalarHex`, `importKey`, `importKeyErr`, `readEntry` follow the plugin suite's helper conventions from earlier tasks.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/plugin/ -run 'TestImport_' -v`
Expected: FAIL — import operation undefined.

- [ ] **Step 3: Implement**

Add the `import` operation to the keys `framework.Path` (or a dedicated `kms/keys/<path>/import` path). Validate path via the shared validator FIRST; read `private_key_hex`, `defer` zero its bytes; hex-decode (length 32 → else 400) and validate as a secp256k1 scalar (non-zero, < curve order → else 400); `ParseChains(chains)`; if storage entry exists → `logical.CodedError(409, fmt.Sprintf("key already exists at path %s", name))`; derive EVM address + compressed pubkey; write `KeyEntry{Source:"imported", ImportedAt: ptr(now), Chains: canonical, ...}`. Extend `GET` to emit `source`, `created_at`, `imported_at`. Ensure `KeyEntry` has `Source string`, `ImportedAt *time.Time` (add if absent).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/plugin/ -run 'TestImport_' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/path_keys.go internal/plugin/backend.go internal/plugin/path_keys_import_test.go
git commit -m "feat(plugin): direct key import with source/imported_at/chains metadata"
```

---

### Task 2: `ErrKeyExists` sentinel + KeyInfo metadata fields

**Files:**
- Modify: `pkg/types/errors.go`, `pkg/types/types.go` (`KeyInfo`)
- Test: `pkg/types/errors_test.go` (if present) — else covered by Task 3.

**Interfaces:**
- Produces: `var ErrKeyExists = errors.New("key already exists")`; `KeyInfo.Source string `+"`json:\"source,omitempty\"`"+`, `KeyInfo.ImportedAt *time.Time `+"`json:\"imported_at,omitempty\"`".

- [ ] **Step 1: Add the sentinel and fields**

In `pkg/types/errors.go` add `ErrKeyExists` alongside `ErrPermission`/`ErrNotFound`/`ErrBadRequest`. In `types.go`, add `Source` and `ImportedAt` to `KeyInfo`.

- [ ] **Step 2: Verify compile**

Run: `go build ./pkg/...`
Expected: builds.

- [ ] **Step 3: Commit**

```bash
git add pkg/types/errors.go pkg/types/types.go
git commit -m "feat(types): add ErrKeyExists sentinel and KeyInfo provenance fields"
```

---

### Task 3: Vault client `ImportKey` + `GetKeyInfo`

**Files:**
- Modify: `internal/vault/client.go`
- Test: `internal/vault/client_import_test.go` (new)

**Interfaces:**
- Produces: `ImportKey(ctx, path string, rawKeyBytes []byte, chains []string) error` (POST `kms/keys/<path>/import` with `private_key_hex` + `chains`; `defer` zero `rawKeyBytes`; 409→`ErrKeyExists`, 403→`ErrPermission`, 400→`ErrBadRequest` via `errors.As(*vaultapi.ResponseError)`); `GetKeyInfo(ctx, path) (types.KeyInfo, error)` parsing `source`/`imported_at`.

- [ ] **Step 1: Write the failing test**

Create `internal/vault/client_import_test.go`:

```go
func TestImportKey_409MapsToErrKeyExists_FromStatusAlone(t *testing.T) {
	// Mock plugin returns 409 with a body that does NOT contain "key already exists".
	srv := mockVault(t, map[string]mockResp{
		"POST /v1/kms/keys/payment/prod/alice/import": {status: 409, body: `{"errors":["conflict"]}`},
	})
	c := newClientForTest(t, srv.URL)
	err := c.ImportKey(context.Background(), "payment/prod/alice", bytes32(t), []string{"evm"})
	if !errors.Is(err, types.ErrKeyExists) {
		t.Fatalf("want ErrKeyExists from 409 status, got %v", err)
	}
}

func TestGetKeyInfo_ParsesProvenance(t *testing.T) {
	srv := mockVault(t, map[string]mockResp{
		"GET /v1/kms/keys/payment/prod/alice": {status: 200, body: `{"data":{"compressed_pub_key":"` + b64PubKey + `","source":"imported","imported_at":"2026-06-17T00:00:00Z"}}`},
	})
	c := newClientForTest(t, srv.URL)
	ki, err := c.GetKeyInfo(context.Background(), "payment/prod/alice")
	if err != nil || ki.Source != "imported" || ki.ImportedAt == nil {
		t.Fatalf("provenance not parsed: %+v err=%v", ki, err)
	}
}
```

(`mockVault`/`newClientForTest`/`bytes32`/`b64PubKey` mirror the existing `client_typed_test.go` doubles.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/vault/ -run 'TestImportKey_409|TestGetKeyInfo_' -v`
Expected: FAIL — methods missing.

- [ ] **Step 3: Implement**

Add `ImportKey` and `GetKeyInfo`; reuse the existing typed-error classifier; add the 409→`ErrKeyExists` branch. `defer` zero `rawKeyBytes`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/vault/ -run 'TestImportKey_409|TestGetKeyInfo_' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vault/client.go internal/vault/client_import_test.go
git commit -m "feat(vault): ImportKey (409->ErrKeyExists) and GetKeyInfo provenance"
```

---

### Task 4: BIP39/BIP44 Cosmos derivation

**Files:**
- Create: `internal/vault/derive.go` (or `internal/derive/derive.go`), `internal/vault/derive_test.go`
- Modify: `go.mod` (add `go-bip39`, `hdkeychain`)

**Interfaces:**
- Produces: `const DefaultCosmosDerivationPath = "m/44'/118'/0'/0/0"`; `DeriveCosmosPrvKey(mnemonic, derivationPath string) ([]byte, error)` returning a 32-byte secp256k1 private key; validates BIP39 word count (12/24) + wordlist; parses BIP44 path; zeroes intermediates via `defer`.

- [ ] **Step 1: Add deps**

Run: `go get github.com/tyler-smith/go-bip39 github.com/btcsuite/btcd/btcutil/hdkeychain && go mod tidy`
Expected: `go.mod`/`go.sum` updated.

- [ ] **Step 2: Write the failing test (known vector)**

Create `internal/vault/derive_test.go`:

```go
func TestDeriveCosmosPrvKey_KnownVector(t *testing.T) {
	// Standard BIP39 test mnemonic; expected key/address fixed under m/44'/118'/0'/0/0.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	key, err := DeriveCosmosPrvKey(mnemonic, DefaultCosmosDerivationPath)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}
	addr, err := cosmos.DeriveCosmosAddress(compressedPub(t, key), "cosmos")
	if err != nil {
		t.Fatalf("addr: %v", err)
	}
	if addr != knownCosmosAddrForVector { // pin the value computed once and asserted thereafter
		t.Fatalf("addr = %s, want %s", addr, knownCosmosAddrForVector)
	}
}

func TestDeriveCosmosPrvKey_InvalidMnemonicAndPath(t *testing.T) {
	if _, err := DeriveCosmosPrvKey("not a real mnemonic", DefaultCosmosDerivationPath); err == nil {
		t.Fatal("want invalid-mnemonic error")
	}
	if _, err := DeriveCosmosPrvKey("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about", "not/a/path"); err == nil {
		t.Fatal("want invalid-path error")
	}
}
```

(Compute `knownCosmosAddrForVector` once during implementation and pin it.)

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/vault/ -run 'TestDeriveCosmosPrvKey' -v`
Expected: FAIL — undefined.

- [ ] **Step 4: Implement**

`DeriveCosmosPrvKey`: `bip39.IsMnemonicValid` (reject else); `bip39.NewSeed(mnemonic, "")`; `hdkeychain.NewMaster(seed, &chaincfg.MainNetParams)`; parse `m/44'/118'/0'/0/0` into hardened/normal indices and derive; extract the 32-byte private key; `defer` zero seed + intermediate keys. Add a path parser validating the `m/...'` format.

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/vault/ -run 'TestDeriveCosmosPrvKey' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/vault/derive.go internal/vault/derive_test.go
git commit -m "feat(derive): BIP39/BIP44 Cosmos private-key derivation"
```

---

### Task 5: Import service (EVM + Cosmos)

**Files:**
- Create: `internal/vault/import.go`, `internal/vault/import_test.go`

**Interfaces:**
- Produces: `ImportEVMKey(ctx, path, privateKeyHex string, chains []string) (types.KeyInfo, error)`; `ImportCosmosKey(ctx, path, mnemonic, derivationPath, hrp string, chains []string) (types.KeyInfo, error)`. Both call `ImportKey` then `GetKeyInfo`; both `defer` zero all intermediate key bytes.

- [ ] **Step 1: Write the failing tests** (mock vault import + GetKeyInfo): EVM import returns populated `KeyInfo` with `source=imported`; Cosmos import derives, imports, returns bech32 address from `compressed_pub_key`.

- [ ] **Step 2: Run to verify failure** — `go test ./internal/vault/ -run 'TestImportEVMKey|TestImportCosmosKey' -v` → FAIL.

- [ ] **Step 3: Implement** the two functions; EVM parses+validates hex scalar then `ImportKey`; Cosmos calls `DeriveCosmosPrvKey` then `ImportKey`; both fetch `GetKeyInfo`. Zero intermediates.

- [ ] **Step 4: Run to verify pass** — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vault/import.go internal/vault/import_test.go
git commit -m "feat(vault): EVM and Cosmos import services"
```

---

### Task 6: Gateway `/v1/keys/import`

**Files:**
- Modify: `internal/gateway/gateway.go` (`appRoutes:241`, new `importKey` handler)
- Test: `internal/gateway/import_test.go` (new)

**Interfaces:**
- Consumes: `ImportEVMKey`/`ImportCosmosKey`; #2 `chains` validation.
- Produces: `POST /v1/keys/import` + `/keys/import` (dual-mount). Body routes on `chain` → EVM or Cosmos; requires `chains`; first success → 201; `ErrKeyExists`→409; `ErrPermission`→403; `ErrBadRequest`→400. Secrets never logged.

- [ ] **Step 1: Write the failing tests**

Create `internal/gateway/import_test.go`:

```go
func TestImport_EVMReturns201(t *testing.T) {
	srv := newTestServer(t)
	resp := postJSON(t, srv, "/keys/import", `{"chain":"evm","path":"payment/prod/alice","private_key_hex":"`+validHex+`","chains":["evm"]}`)
	assertStatus(t, resp, 201)
}

func TestImport_DuplicateReturns409(t *testing.T) {
	srv := newTestServer(t, withExistingKey("payment/prod/alice"))
	resp := postJSON(t, srv, "/keys/import", `{"chain":"evm","path":"payment/prod/alice","private_key_hex":"`+validHex+`","chains":["evm"]}`)
	assertStatus(t, resp, 409)
}

func TestImport_MissingChainsReturns400(t *testing.T) {
	srv := newTestServer(t)
	resp := postJSON(t, srv, "/keys/import", `{"chain":"evm","path":"payment/prod/alice","private_key_hex":"`+validHex+`"}`)
	assertStatus(t, resp, 400)
}

func TestImport_SecretsNotLogged(t *testing.T) {
	logs := captureLogs(t)
	srv := newTestServer(t, withLogger(logs))
	_ = postJSON(t, srv, "/keys/import", `{"chain":"evm","path":"payment/prod/alice","private_key_hex":"`+validHex+`","chains":["evm"]}`)
	if strings.Contains(logs.String(), validHex) {
		t.Fatal("private key leaked into logs")
	}
}
```

- [ ] **Step 2: Run to verify failure** — FAIL (route absent).

- [ ] **Step 3: Implement** the handler: decode (strict), validate `chains` via `types.ParseChains` (400 on miss), route on `chain` to the import service, map typed errors to status, 201 on success. Ensure the request body / secret fields are never passed to any logger. Register both paths in `appRoutes` with auth + rate limiter.

- [ ] **Step 4: Run to verify pass** — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/gateway.go internal/gateway/import_test.go
git commit -m "feat(gateway): /v1/keys/import with typed errors and secret redaction"
```

---

### Task 7: Gateway `/v1/sign/evm/safe`

**Files:**
- Modify: `internal/gateway/gateway.go` (new `signEVMSafe` handler, route)
- Test: `internal/gateway/safe_sign_test.go` (new)

**Interfaces:**
- Consumes: EVM signer `SignEIP712Digest`; #2 `authorizeChain(path,"evm")`.
- Produces: `POST /v1/sign/evm/safe` + bare alias. Validates 32-byte `safe_tx_hash`; chain-authz 403 on non-evm-tagged key; returns `{signature, signer_address}`; audit log with `request_id`.

- [ ] **Step 1: Write the failing tests** — success returns 65-byte signature + audit log contains `request_id` and `safe_tx_hash`; non-32-byte hash → 400; non-hex → 400; cosmos-only key → 403.

- [ ] **Step 2: Run to verify failure** — FAIL.

- [ ] **Step 3: Implement** — decode `safe_tx_hash`, enforce 32 bytes; `authorizeChain(ctx, path, types.ChainEVM)` (reuse #2 helper: deny→403, lookup error→503); call `SignEIP712Digest`; emit structured `info` audit log (`key_path`, `signer_address`, `safe_tx_hash`, `request_id`); respond `{signature, signer_address}`. Register routes.

- [ ] **Step 4: Run to verify pass** — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/gateway.go internal/gateway/safe_sign_test.go
git commit -m "feat(gateway): /v1/sign/evm/safe (safeTxHash) with chain-authz and audit"
```

---

### Task 8: Gateway `/v1/sign/cosmos/partial`

**Files:**
- Modify: `internal/gateway/gateway.go` (new `signCosmosPartial` handler, route)
- Test: `internal/gateway/partial_sign_test.go` (new)

**Interfaces:**
- Consumes: Cosmos signer (`SortJSON` AMINO canonicalisation); #2 `authorizeChain(path,"cosmos")`.
- Produces: `POST /v1/sign/cosmos/partial` + bare alias. Validates `signer_index` bounds + gateway-pubkey == `multisig_pubkeys[signer_index]`; returns `{signature(SignatureV2 b64), pub_key, signer_index}`.

- [ ] **Step 1: Write the failing tests** — DIRECT success; AMINO success (canonical); AMINO duplicate keys → 400; out-of-range `signer_index` → 400; pubkey mismatch → 400; evm-only key → 403; unsupported mode → 400.

- [ ] **Step 2: Run to verify failure** — FAIL.

- [ ] **Step 3: Implement** — parse request (`key_path`, `sign_mode`, `sign_doc`, `signer_index`, `multisig_pubkeys`, `threshold`); `authorizeChain(ctx, path, types.ChainCosmos)`; validate index range and `gatewayPubKey == multisig_pubkeys[signer_index]` (else 400); dispatch to the Cosmos signer; return the `SignatureV2` + `pub_key` + `signer_index`. Register routes.

- [ ] **Step 4: Run to verify pass** — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/gateway.go internal/gateway/partial_sign_test.go
git commit -m "feat(gateway): /v1/sign/cosmos/partial with signer-index and pubkey validation"
```

---

### Task 9: CLI `keys import`

**Files:**
- Modify: `cmd/kms-wrapper/` (new `keys import` command)
- Test: `cmd/kms-wrapper/import_test.go` (new)

**Interfaces:**
- Produces: `kms-wrapper keys import --path --chain evm|cosmos --chains evm,cosmos [--private-key | --mnemonic --derivation-path --hrp] [--yes]`. `--private-key` requires `chain=evm`; `--mnemonic` requires `chain=cosmos`; exactly one of the two; `--chains` required. Cosmos prints derived address for confirmation unless `--yes`/non-TTY.

- [ ] **Step 1: Write the failing tests** — EVM import success; Cosmos import `--yes` success; missing `--chains` → non-zero + subset message; `--private-key` with `chain=cosmos` → error; both `--private-key` and `--mnemonic` → error; confirmation rejected → abort non-zero, no import.

- [ ] **Step 2: Run to verify failure** — FAIL.

- [ ] **Step 3: Implement** — cobra command with the flags; mutex validation; `--chains` parsed + required; route to `ImportEVMKey`/`ImportCosmosKey`; print derived address + confirmation prompt for Cosmos (skip on `--yes`/non-TTY); `defer` zero secret inputs; help text warns about mnemonic shell history (`--mnemonic "$(cat -)"`).

- [ ] **Step 4: Run to verify pass** — PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/kms-wrapper/
git commit -m "feat(cli): keys import (EVM key / Cosmos mnemonic) with required --chains"
```

---

### Task 10: CLI `sign cosmos partial` + `sign evm safe`

**Files:**
- Modify: `cmd/kms-wrapper/` (two new commands)
- Test: `cmd/kms-wrapper/partial_safe_test.go` (new)

**Interfaces:**
- Produces: `kms-wrapper sign cosmos partial --path --mode --sign-doc --signer-index --multisig-pubkeys --threshold`; `kms-wrapper sign evm safe --path --safe-tx-hash`. Both use an outer-scope `var err error` declared before any `switch`; inner blocks use distinct locals (`decErr`) — no err-shadowing.

- [ ] **Step 1: Write the failing tests** — for BOTH commands, a fake signer returning an error → assert **non-zero exit AND empty stdout** (err-shadowing regression). Plus: success, missing flags, invalid input.

- [ ] **Step 2: Run to verify failure** — FAIL.

- [ ] **Step 3: Implement** both commands with the outer-scope `err` discipline; wire to the gateway endpoints / internal packages consistent with existing `sign` subcommands.

- [ ] **Step 4: Run to verify pass** — PASS (including the error-path empty-stdout assertion).

- [ ] **Step 5: Commit**

```bash
git add cmd/kms-wrapper/
git commit -m "feat(cli): sign cosmos partial and sign evm safe subcommands"
```

---

### Task 11: Vault policy, config, README

**Files:**
- Modify: `vault/init.sh` (+ `vault/policy-project.hcl`), `.env.example`, `README.md`

**Interfaces:** none (ops/docs).

- [ ] **Step 1: Extend the project policy**

Add to the policy template: `path "kms/keys/+/import" { capabilities = ["create"] }`. Keep globs project-scoped (`kms/keys/<project>/*`, `kms/sign/<project>/*`).

- [ ] **Step 2: Extend the `init.sh` policy smoke test**

Assert the issued token can import under its own project (`kms/keys/<project>/prod/x/import`) but NOT under another project (403).

- [ ] **Step 3: Config + README**

Remove `KMS_METADATA_KV_MOUNT` from `.env.example` (D9 plugin-native). README: Vault 1.17+ note, new `kms/keys/+/import` policy path, import CLI UX (EVM + Cosmos + shell-history warning), partial-sign flow (2-of-3), Safe-sign flow.

- [ ] **Step 4: Verify bootstrap parses**

Run: `bash -n vault/init.sh`
Expected: no syntax errors.

- [ ] **Step 5: Commit**

```bash
git add vault/init.sh vault/policy-project.hcl .env.example README.md
git commit -m "docs(policy,readme): import capability, plugin-native metadata, multisig flows"
```

---

### Task 12: OpenAPI annotations + regenerate

**Files:**
- Modify: `internal/gateway/gateway.go` (swagger annotations on the 3 new handlers)
- Modify (generated): `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`

- [ ] **Step 1: Annotate** the three new handlers: request/response schemas, `201` on first import, `409` on duplicate, bearer security, `/v1/` paths.

- [ ] **Step 2: Regenerate + check**

Run: `make swagger && make swagger-check`
Expected: clean; spec advertises `/v1/keys/import`, `/v1/sign/cosmos/partial`, `/v1/sign/evm/safe` with 201/409 documented.

- [ ] **Step 3: Full suite**

Run: `go test ./... && make lint`
Expected: green.

- [ ] **Step 4: Commit**

```bash
git add internal/gateway/gateway.go docs/
git commit -m "docs(swagger): annotate and regenerate import + multisig routes"
```

---

### Task 13: E2E operator runbook + verification

**Files:**
- Create: `docs/e2e-runbook.md`

- [ ] **Step 1: Write the runbook**

Sections: prerequisites; `make build-plugin && make dev-up` expected output; key lifecycle (`create`/`show` with `--chains`); EVM signing (raw/personal/EIP-712) + recovered-address verification; Cosmos signing (DIRECT/AMINO) + cosmjs verification; EVM key import (before/after `keys show` showing `source: imported`); Cosmos mnemonic import (`--mnemonic "$(cat -)"`, confirmation, `--yes`); Cosmos 2-of-3 partial multisig (assemble `MultiSignature` with cosmjs); EVM Safe sign (compute `safeTxHash` with `@safe-global/protocol-kit`); a 10-command "verify your setup" checklist.

- [ ] **Step 2: Live E2E**

```bash
make scrub-env && make dev-down && make dev-up
export VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root KMS_GATEWAY_TOKEN=dev
# EVM import
go run ./cmd/kms-wrapper keys import --chain evm --chains evm --path payment/prod/imported \
  --private-key <test-hex>
go run ./cmd/kms-wrapper keys show --path payment/prod/imported   # source: imported
# Safe sign (evm-tagged → 200)
curl -fsS -X POST -H "Authorization: Bearer $KMS_GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"key_path":"payment/prod/imported","safe_tx_hash":"0x'"$(python3 -c 'print("ab"*32)')"'"}' \
  http://127.0.0.1:8080/v1/sign/evm/safe
```
Expected: import → 201; `show` reports `source: imported`; Safe sign → 200 with 65-byte signature; a cosmos-only key against `/v1/sign/evm/safe` → 403 (chain-authz from #2).

- [ ] **Step 3: Confirm docs surface**

```bash
curl -fsS http://127.0.0.1:8080/swagger/doc.json | grep -o '/v1/keys/import\|/v1/sign/evm/safe\|/v1/sign/cosmos/partial'
```
Expected: all three present.

- [ ] **Step 4: Record verification result** in the PR description.

- [ ] **Step 5: Commit**

```bash
git add docs/e2e-runbook.md
git commit -m "docs: end-to-end operator runbook for import and multisig"
```

---

## Self-Review

- **Spec coverage:** D7 plugin import → Task 1; D8 derive-then-import → Task 4/5; D9 metadata → Task 1/2/3; D10 partial-sign → Task 8/10; D11 Safe sign → Task 7/10; D12 routes → Tasks 6/7/8; D13 CLI → Tasks 9/10; policy → Task 11; docs/regen → Task 12; runbook → Task 13. Reconciliation: `{environment}` paths used throughout; `chains` required on import (Task 1/6/9); `authorizeChain` on the two sign routes (Task 7/8). Decisions: default `m/44'/118'/0'/0/0` (Task 4); `--chains` required (Task 9). Hardening: zeroization (Tasks 1/3/5/9), never-log (Task 6), 409 fail-safe (Tasks 1/3/6), strict validation (Tasks 1/7/8), audit logs (Task 7/8). All map.
- **Placeholder scan:** none — concrete signatures, error strings, and commands throughout. Test-helper names flagged to match the existing suites.
- **Type consistency:** `ImportKey(ctx,path,rawKeyBytes,chains)` (Task 3) consumed by Task 5; `GetKeyInfo` (Task 3) consumed by Task 5; `ImportEVMKey`/`ImportCosmosKey` (Task 5) consumed by Tasks 6/9; `ErrKeyExists` (Task 2) consumed by Tasks 3/6; `DeriveCosmosPrvKey`+`DefaultCosmosDerivationPath` (Task 4) consumed by Tasks 5/9; `authorizeChain` (from #2) consumed by Tasks 7/8.
- **Dependency note:** requires #1 + #2 merged; all paths `payment/prod/...`, imported keys carry `chains`.

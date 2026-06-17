# refactor-swagger-schema-names Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite the OpenAPI schema prefix `github_com_ryan-truong_kms-wrapper_pkg_types.` → `kms-wrapper_pkg_types.` in `cmd/swagger-postprocess`, fix the dangling EVM discriminator mapping in the same pass, and add a regression guard that fails CI if the long prefix returns or any discriminator mapping `$ref` dangles.

**Architecture:** A deterministic transform inside the existing `normalizeSpec` pipeline. `renameSchemaPrefix` runs before `injectEVMDiscriminator`; `injectEVMDiscriminator` is updated to emit short-prefix mapping refs; `validateSpecRefs` asserts no dangling mapping refs. No `pkg/types` rename, no runtime change.

**Tech Stack:** Go 1.25+, `cmd/swagger-postprocess` (stdlib `encoding/json` over `map[string]any`), `swaggo/swag` v2, Make.

## Global Constraints

- Old prefix (verbatim): `github_com_ryan-truong_kms-wrapper_pkg_types`
- New prefix (verbatim): `kms-wrapper_pkg_types`
- `$ref` form is `#/components/schemas/<prefix>.<TypeName>`; the `#/components/schemas/` segment is preserved, only the `<prefix>` changes.
- No edits to `pkg/types/types.go`, handler signatures, Vault, or CLI.
- Discriminator mapping keys remain `raw_tx`/`personal_message`/`eip712_digest`; only the ref values change.
- After regen: zero occurrences of the long prefix in `docs/swagger.json` and `docs/docs.go`; every `discriminator.mapping` value resolves to an existing `components.schemas` key.

---

### Task 1: `renameSchemaPrefix` + fixed discriminator mapping

**Files:**
- Modify: `cmd/swagger-postprocess/main.go` (`normalizeSpec:57`, `injectEVMDiscriminator:117-119`)
- Test: `cmd/swagger-postprocess/main_test.go`

**Interfaces:**
- Produces: `func renameSchemaPrefix(spec map[string]any, oldPrefix, newPrefix string)` — renames `components.schemas` keys and rewrites every nested `$ref` string containing `#/components/schemas/<oldPrefix>`.

- [ ] **Step 1: Write the failing test**

Add to `cmd/swagger-postprocess/main_test.go`:

```go
func TestRenameSchemaPrefix_KeysAndRefs(t *testing.T) {
	old := "github_com_ryan-truong_kms-wrapper_pkg_types"
	spec := map[string]any{
		"components": map[string]any{
			"schemas": map[string]any{
				old + ".KeyInfo": map[string]any{"type": "object"},
				old + ".EVMSignRawTxRequest": map[string]any{
					"properties": map[string]any{
						"nested": map[string]any{"$ref": "#/components/schemas/" + old + ".KeyInfo"},
					},
				},
			},
		},
		"paths": map[string]any{
			"/v1/sign/evm": map[string]any{
				"post": map[string]any{
					"requestBody": map[string]any{"content": map[string]any{
						"application/json": map[string]any{"schema": map[string]any{
							"oneOf": []any{map[string]any{"$ref": "#/components/schemas/" + old + ".EVMSignRawTxRequest"}},
						}},
					}},
				},
			},
		},
	}

	renameSchemaPrefix(spec, old, "kms-wrapper_pkg_types")

	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	if _, ok := schemas["kms-wrapper_pkg_types.KeyInfo"]; !ok {
		t.Fatal("KeyInfo key not renamed")
	}
	if _, ok := schemas[old+".KeyInfo"]; ok {
		t.Fatal("old KeyInfo key still present")
	}
	nestedRef := schemas["kms-wrapper_pkg_types.EVMSignRawTxRequest"].(map[string]any)["properties"].(map[string]any)["nested"].(map[string]any)["$ref"]
	if nestedRef != "#/components/schemas/kms-wrapper_pkg_types.KeyInfo" {
		t.Fatalf("nested $ref not rewritten: %v", nestedRef)
	}
	oneOfRef := spec["paths"].(map[string]any)["/v1/sign/evm"].(map[string]any)["post"].(map[string]any)["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["oneOf"].([]any)[0].(map[string]any)["$ref"]
	if oneOfRef != "#/components/schemas/kms-wrapper_pkg_types.EVMSignRawTxRequest" {
		t.Fatalf("oneOf $ref not rewritten: %v", oneOfRef)
	}
}

func TestInjectEVMDiscriminator_UsesShortPrefix(t *testing.T) {
	op := map[string]any{"requestBody": map[string]any{"content": map[string]any{
		"application/json": map[string]any{"schema": map[string]any{"oneOf": []any{}}},
	}}}
	injectEVMDiscriminator(op)
	mapping := op["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["discriminator"].(map[string]any)["mapping"].(map[string]any)
	if mapping["raw_tx"] != "#/components/schemas/kms-wrapper_pkg_types.EVMSignRawTxRequest" {
		t.Fatalf("raw_tx mapping wrong: %v", mapping["raw_tx"])
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/swagger-postprocess/ -run 'TestRenameSchemaPrefix|TestInjectEVMDiscriminator_UsesShort' -v`
Expected: FAIL — `renameSchemaPrefix` undefined; discriminator still uses `types.` prefix.

- [ ] **Step 3: Implement `renameSchemaPrefix` and fix the mapping**

Add to `main.go`:

```go
// renameSchemaPrefix renames components.schemas keys from oldPrefix to
// newPrefix and rewrites every nested $ref that points at them.
func renameSchemaPrefix(spec map[string]any, oldPrefix, newPrefix string) {
	components, _ := spec["components"].(map[string]any)
	if schemas, ok := components["schemas"].(map[string]any); ok {
		for key, val := range schemas {
			if strings.HasPrefix(key, oldPrefix+".") {
				schemas[newPrefix+"."+strings.TrimPrefix(key, oldPrefix+".")] = val
				delete(schemas, key)
			}
		}
	}
	oldRef := "#/components/schemas/" + oldPrefix + "."
	newRef := "#/components/schemas/" + newPrefix + "."
	rewriteRefs(spec, oldRef, newRef)
}

// rewriteRefs walks an arbitrary decoded-JSON value and rewrites every
// string $ref / discriminator-mapping value beginning with oldRef.
func rewriteRefs(node any, oldRef, newRef string) {
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			if s, ok := v.(string); ok && strings.HasPrefix(s, oldRef) {
				n[k] = newRef + strings.TrimPrefix(s, oldRef)
				continue
			}
			rewriteRefs(v, oldRef, newRef)
		}
	case []any:
		for _, v := range n {
			rewriteRefs(v, oldRef, newRef)
		}
	}
}
```

In `normalizeSpec`, add as the first line after `normalizeServers(spec)` (i.e. before the discriminator injection loop):

```go
	renameSchemaPrefix(spec, "github_com_ryan-truong_kms-wrapper_pkg_types", "kms-wrapper_pkg_types")
```

Update the mapping in `injectEVMDiscriminator` (lines 117-119) to:

```go
				"raw_tx":           "#/components/schemas/kms-wrapper_pkg_types.EVMSignRawTxRequest",
				"personal_message": "#/components/schemas/kms-wrapper_pkg_types.EVMSignPersonalMessageRequest",
				"eip712_digest":    "#/components/schemas/kms-wrapper_pkg_types.EVMSignEIP712Request",
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./cmd/swagger-postprocess/ -run 'TestRenameSchemaPrefix|TestInjectEVMDiscriminator_UsesShort' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/swagger-postprocess/main.go cmd/swagger-postprocess/main_test.go
git commit -m "feat(swagger-postprocess): short schema prefix + fixed discriminator mapping"
```

---

### Task 2: `validateSpecRefs` dangling-mapping guard

**Files:**
- Modify: `cmd/swagger-postprocess/main.go` (call from `run`/`normalizeSpec` and surface a non-nil error to `main`)
- Test: `cmd/swagger-postprocess/main_test.go`

**Interfaces:**
- Produces: `func validateSpecRefs(spec map[string]any) error` — returns a descriptive error if any `discriminator.mapping` `$ref` does not resolve to an existing `components.schemas` key.

- [ ] **Step 1: Write the failing test**

```go
func TestValidateSpecRefs_DanglingMappingFails(t *testing.T) {
	spec := map[string]any{
		"components": map[string]any{"schemas": map[string]any{
			"kms-wrapper_pkg_types.EVMSignRawTxRequest": map[string]any{},
		}},
		"paths": map[string]any{"/v1/sign/evm": map[string]any{"post": map[string]any{
			"requestBody": map[string]any{"content": map[string]any{
				"application/json": map[string]any{"schema": map[string]any{
					"discriminator": map[string]any{"mapping": map[string]any{
						"raw_tx": "#/components/schemas/kms-wrapper_pkg_types.MISSING",
					}},
				}},
			}},
		}}},
	}
	if err := validateSpecRefs(spec); err == nil {
		t.Fatal("expected dangling-ref error")
	}
}

func TestValidateSpecRefs_AllResolveOK(t *testing.T) {
	spec := map[string]any{
		"components": map[string]any{"schemas": map[string]any{
			"kms-wrapper_pkg_types.EVMSignRawTxRequest": map[string]any{},
		}},
		"paths": map[string]any{"/v1/sign/evm": map[string]any{"post": map[string]any{
			"requestBody": map[string]any{"content": map[string]any{
				"application/json": map[string]any{"schema": map[string]any{
					"discriminator": map[string]any{"mapping": map[string]any{
						"raw_tx": "#/components/schemas/kms-wrapper_pkg_types.EVMSignRawTxRequest",
					}},
				}},
			}},
		}}},
	}
	if err := validateSpecRefs(spec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/swagger-postprocess/ -run 'TestValidateSpecRefs' -v`
Expected: FAIL — `validateSpecRefs` undefined.

- [ ] **Step 3: Implement**

```go
// validateSpecRefs fails if any discriminator.mapping $ref does not resolve
// to an existing components.schemas key. Guards against swag output drift
// silently re-breaking codegen.
func validateSpecRefs(spec map[string]any) error {
	components, _ := spec["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	exists := func(ref string) bool {
		const p = "#/components/schemas/"
		if !strings.HasPrefix(ref, p) {
			return true // not a local schema ref; not our concern
		}
		_, ok := schemas[strings.TrimPrefix(ref, p)]
		return ok
	}
	var bad []string
	var walk func(any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if disc, ok := n["discriminator"].(map[string]any); ok {
				if mapping, ok := disc["mapping"].(map[string]any); ok {
					for k, v := range mapping {
						if s, ok := v.(string); ok && !exists(s) {
							bad = append(bad, k+" -> "+s)
						}
					}
				}
			}
			for _, v := range n {
				walk(v)
			}
		case []any:
			for _, v := range n {
				walk(v)
			}
		}
	}
	walk(spec)
	if len(bad) > 0 {
		return fmt.Errorf("dangling discriminator mapping refs: %s", strings.Join(bad, ", "))
	}
	return nil
}
```

Call it in `run()` after `normalizeSpec(spec)` and before writing output; on error, return it so `main` exits non-zero (fails `make swagger`/`make swagger-check`). Ensure `fmt` is imported.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./cmd/swagger-postprocess/ -run 'TestValidateSpecRefs' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/swagger-postprocess/main.go cmd/swagger-postprocess/main_test.go
git commit -m "feat(swagger-postprocess): guard against dangling discriminator mapping refs"
```

---

### Task 3: Regenerate docs and verify

**Files:**
- Modify (generated): `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`

**Interfaces:** none.

- [ ] **Step 1: Regenerate**

Run: `make swagger`
Expected: success (the new `validateSpecRefs` does not error — all mapping refs resolve).

- [ ] **Step 2: Assert long prefix is gone and refs resolve**

Run:
```bash
! grep -q github_com_ryan-truong_kms-wrapper_pkg_types docs/swagger.json && \
! grep -q github_com_ryan-truong_kms-wrapper_pkg_types docs/docs.go && echo "PREFIX CLEAN"
```
Expected: prints `PREFIX CLEAN`.

- [ ] **Step 3: Confirm regeneration is clean and full suite is green**

Run: `make swagger-check && go test ./... && make lint`
Expected: all green (confirms `pkg/types`/handlers untouched).

- [ ] **Step 4: Validate OpenAPI**

Run: `go run ./cmd/swagger-postprocess --help >/dev/null 2>&1 || true` then validate the doc is OpenAPI 3.0.x — e.g. inspect `docs/swagger.json` top-level `"openapi": "3.0.3"` and that `paths./v1/sign/evm.post.requestBody...discriminator.mapping` values all begin with `#/components/schemas/kms-wrapper_pkg_types.`.

Run: `grep -o '"openapi": *"3.0.3"' docs/swagger.json`
Expected: one match.

- [ ] **Step 5: Commit**

```bash
git add docs/docs.go docs/swagger.json docs/swagger.yaml
git commit -m "docs(swagger): regenerate with short schema prefix and resolved discriminator"
```

---

## Self-Review

- **Spec coverage:** "Schema component names use a stable short prefix" → Task 1 (`renameSchemaPrefix`) + Task 3 assertions. "Refs rewritten consistently" → Task 1 (`rewriteRefs` over nested nodes). "Repo-path prefix never reappears" → Task 2/3 guard + grep. "Discriminator drives codegen" (MODIFIED, short prefix) → Task 1 mapping fix + Task 2 resolve-guard. All map.
- **Placeholder scan:** none — every step shows concrete Go and commands.
- **Type consistency:** `renameSchemaPrefix(spec, old, new)`, `rewriteRefs(node, oldRef, newRef)`, `validateSpecRefs(spec) error` — names used identically in implementation, call sites (`normalizeSpec`/`run`), and tests. Prefix string literals identical across Task 1 and Task 2.

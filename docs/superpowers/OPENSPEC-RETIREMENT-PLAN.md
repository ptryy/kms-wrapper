# OpenSpec Retirement Plan (proposal — not yet executed)

**Date:** 2026-06-17
**Status:** PROPOSED — awaiting approval. Nothing in `openspec/` has been deleted.

This is Step 4 of the OpenSpec → Superpowers migration: a deletion plan for the remaining
`openspec/` directory with a knowledge-loss audit. The guiding rule: **no critical knowledge is lost.**

## Knowledge-loss audit

| Path | What it is | Captured by migration? | Verdict |
|------|-----------|------------------------|---------|
| `openspec/project.md` | Unfilled template (no real content) | Fully — synthesized into `/CONSTITUTION.md` | ✅ Safe to delete |
| `openspec/AGENTS.md` | OpenSpec **tool** workflow instructions | N/A — tool-specific, not project knowledge | ⚠️ Delete only after CLAUDE.md block is updated |
| `openspec/specs/` (7 capability specs) | **The authoritative behavioral contract of the *implemented* system** — WHEN/THEN scenarios for key-path-policy, vault-backend, evm-signer, cosmos-signer, rest-gateway, cli, api-docs | **Only summarized** in CONSTITUTION §3. The detailed scenarios are NOT reproduced anywhere else. | ⛔ **DO NOT DELETE — RELOCATE.** This is the living spec; losing it loses the acceptance criteria for everything already built. |
| `openspec/changes/{4 active}` | Legacy proposals/designs/spec-deltas/tasks for the 4 pending changes | Superseded by `docs/superpowers/specs/*-design.md` + `docs/superpowers/plans/*.md`. Verbatim spec-delta scenarios are referenced but not all reproduced. | ⚠️ **ARCHIVE (lossless), don't delete** until the changes are implemented |

### Other references that would dangle after deletion
- **`/CLAUDE.md`** — the managed "OpenSpec Instructions" block points at `@/openspec/AGENTS.md`.
- **`README.md`** — line ~11 links `openspec/changes/multi-chain-kms-gateway/design.md` (now under `docs/archive/`).
- **OpenSpec tooling** — `.claude/commands/opsx/*`, `.claude/skills/openspec-*`, `.qwen/skills/openspec-*` are OpenSpec slash-commands/skills that target `openspec/`. (Tooling, not knowledge — optional cleanup.)

> ⚠️ Note: `openspec/specs/key-path-policy` and `vault-backend` still describe **Vault Transit**
> (`transit/keys/...`, `ecdsa-p256k1`) while the system uses the custom plugin (`kms/...`). See
> `CONSTITUTION.md §7`. Relocation is the moment to either keep them as-is (historical) or update to
> plugin reality — flagged below as a decision.

## Proposed actions (in order)

1. **Relocate the living spec** — `git mv openspec/specs/ docs/specs/` (preserves history). Add
   `docs/specs/README.md` noting these are the current system's behavioral contract, migrated from
   OpenSpec on 2026-06-17, and that `update-key-path-scheme` (#1) will update the Transit→plugin
   wording on implementation.
2. **Archive the 4 active changes (lossless)** — `git mv openspec/changes/<each> docs/archive/legacy-openspec-changes/<each>`
   for `update-key-path-scheme`, `add-key-chain-capability`, `refactor-swagger-schema-names`,
   `key-import-and-multisig`. Add a pointer in each Superpowers plan back to its archived legacy source.
3. **Update `/CLAUDE.md`** — replace the managed OpenSpec block with a Superpowers pointer
   (`/CONSTITUTION.md`, `docs/superpowers/specs/`, `docs/superpowers/plans/`). Since the block is
   marked "Keep this managed block so 'openspec update' can refresh", removing it ends OpenSpec's
   ownership of CLAUDE.md — intended.
4. **Update `README.md`** — repoint the `openspec/changes/...` link to `docs/archive/...` and add a
   one-line note that the project now uses Superpowers (`/CONSTITUTION.md`).
5. **Delete the now-empty remainder** — `git rm openspec/project.md openspec/AGENTS.md` and remove the
   empty `openspec/` tree. (All real content has been relocated/archived by steps 1–2.)
6. **(Optional, separate decision) Remove OpenSpec tooling** — `.claude/commands/opsx/*`,
   `.claude/skills/openspec-*`, `.qwen/skills/openspec-*`. Out of strict scope; propose as a
   follow-up so the retirement PR stays focused.

## Result after execution

```
/CONSTITUTION.md                      # master Project Vision
docs/specs/                           # current system behavioral contract (was openspec/specs/)
docs/superpowers/specs/               # 4 migrated change designs
docs/superpowers/plans/               # 4 granular implementation plans
docs/archive/                         # 11 implemented changes (Migrated/Legacy)
docs/archive/legacy-openspec-changes/ # 4 pending changes' legacy artifacts (superseded by plans)
# openspec/ removed
```

**Net knowledge delta: zero.** Every artifact is either relocated, archived, or (for the empty
template + tool instructions) provably redundant.

## Decision needed before executing

- **Spec wording at relocation:** keep `docs/specs/` byte-for-byte (historical, still mentions
  Transit) **or** update the Transit→plugin wording now? Recommendation: **keep byte-for-byte**;
  the wording update belongs to `update-key-path-scheme` (#1) implementation so the change is tracked
  in one place.

# Next Steps — Implementing the Migrated Plans

The OpenSpec → Superpowers migration (2026-06-17) produced four ready-to-implement plans under
[`docs/superpowers/plans/`](./plans/), governed by [`/CONSTITUTION.md`](../../CONSTITUTION.md).
This file holds copy-paste prompts to implement them in a fresh session.

## Build order (dependencies)

| # | Plan | Depends on | When |
|---|------|-----------|------|
| 1 | `2026-06-17-update-key-path-scheme.md` | — | **FIRST** (BREAKING) |
| 2 | `2026-06-17-add-key-chain-capability.md` | #1 | after #1 merged |
| 4 | `2026-06-17-refactor-swagger-schema-names.md` | — | any time (independent) |
| 3 | `2026-06-17-key-import-and-multisig.md` | #1 **and** #2 | **LAST** |

> #1 first also clears the Transit→plugin wording drift noted in `CONSTITUTION.md §7` before #3.

## Kickoff prompt (start with #1)

```
Implement the plan at docs/superpowers/plans/2026-06-17-update-key-path-scheme.md
using the superpowers:subagent-driven-development skill. Work task-by-task in
order, TDD as written, and stop for my review between tasks. Read /CONSTITUTION.md
first for project rules. Do not push or merge; branch off main first.
```

## Per-change prompts (swap the filename)

**#2 — add-key-chain-capability** (after #1 merged):
```
Implement docs/superpowers/plans/2026-06-17-add-key-chain-capability.md with
superpowers:subagent-driven-development. This depends on update-key-path-scheme
being merged (uses {environment} paths). Read /CONSTITUTION.md first. Task-by-task,
TDD, review between tasks. Branch off main; don't push/merge.
```

**#4 — refactor-swagger-schema-names** (independent):
```
Implement docs/superpowers/plans/2026-06-17-refactor-swagger-schema-names.md with
superpowers:subagent-driven-development. Read /CONSTITUTION.md first. TDD, review
between tasks. Branch off main; don't push/merge.
```

**#3 — key-import-and-multisig** (after #1 + #2 merged):
```
Implement docs/superpowers/plans/2026-06-17-key-import-and-multisig.md with
superpowers:subagent-driven-development. Depends on update-key-path-scheme AND
add-key-chain-capability being merged. Read /CONSTITUTION.md first. TDD, review
between tasks. Branch off main; don't push/merge.
```

## Notes

- **Execution skill:** the plan headers recommend `superpowers:subagent-driven-development` (fresh
  subagent per task + review checkpoints). To drive it inline in one session instead, use
  `superpowers:executing-plans`.
- Each plan's header already names the required skill and per-task TDD steps, so even a terse
  "implement `<plan path>`" works.
- After each change: `go test ./...`, `make lint`, and `make swagger-check` must be green before merge
  (per `CONSTITUTION.md §4.7`).

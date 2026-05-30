## Context

Current behavior fails early with:

`read config: open ~/.kms-wrapper/config.yaml: no such file or directory`

That happens before env-only runtime config can be used. The root cause is that config loading treats a missing file at a concrete path as a hard read error.

## Decisions

### D1: Config file presence is optional

If `--config` points to a file that does not exist:

- print a warning containing the missing path
- continue startup using env/default configuration sources

This applies to both the default path and explicitly passed `--config` paths.

### D2: Fatal vs non-fatal config read errors

- **Non-fatal**: file does not exist (`ENOENT`)
- **Fatal**: file exists but cannot be parsed/read for other reasons (invalid YAML, permission denied, etc.)

### D3: Preserve strict runtime validation

After fallback resolution:

- if `vault.addr`, `vault.token`, or `gateway.token` are missing, startup fails with validation errors
- no silent success-shaped fallback is allowed

### D4: Health output clarity

`kms-wrapper health` should not emit `Vault: UNREACHABLE ()` for config-load/validation issues unrelated to network reachability. Connectivity status output is only for real Vault client connectivity checks.

## Config resolution model

```text
defaults
  -> config file (if present and readable)
    -> env overrides (KMS_* and VAULT_* compatibility vars)
      -> runtime validation (required fields)
```

Missing file means the middle step is skipped with warning.

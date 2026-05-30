## 1. Config loader behavior

- [ ] 1.1 Update `internal/config.Load(path)` so missing config files (`ENOENT`) are non-fatal and fall back to env/defaults
- [ ] 1.2 Preserve fatal behavior for malformed/unreadable files (invalid YAML, permission errors, etc.)
- [ ] 1.3 Add loader metadata (or equivalent signal) so CLI can distinguish "missing file fallback" from hard config errors

## 2. CLI startup + health UX

- [ ] 2.1 Update `cmd/kms-wrapper` startup path to print a warning when config file is missing and fallback is used
- [ ] 2.2 Ensure warning includes the path and states fallback to env/defaults
- [ ] 2.3 Update `health` command error flow so config validation errors are reported as config/runtime issues, not Vault connectivity failures

## 3. Tests

- [ ] 3.1 Add `internal/config` tests for:
  - missing default path fallback
  - missing explicit `--config` path fallback
  - malformed YAML remains fatal
- [ ] 3.2 Add CLI tests for env-only startup without config file:
  - `health` succeeds when env vars are set
  - command fails with validation error when required env vars are absent

## 4. Documentation

- [ ] 4.1 Update README quickstart to explicitly state config file is optional
- [ ] 4.2 Document warning behavior and precedence: defaults -> file (if present) -> env overrides

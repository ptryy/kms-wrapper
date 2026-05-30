## Why

`kms-wrapper` currently defaults `--config` to `~/.kms-wrapper/config.yaml`. When that file is absent, command startup fails before environment variable fallback is applied, even when `VAULT_ADDR`, `VAULT_TOKEN`, and `KMS_GATEWAY_TOKEN` are set.

This conflicts with the intended operator workflow in README quickstart and the design direction that env vars should override and support container/CI usage without requiring local config files.

## What Changes

- Treat missing config files as **non-fatal** for both default and explicit `--config` paths.
- Emit a warning when the config file is missing, then continue with env/default values.
- Keep unreadable/malformed config files as fatal errors.
- Keep runtime validation strict: if required values are still missing after fallback, fail with descriptive validation errors.
- Ensure `health` output distinguishes config/runtime validation failures from Vault connectivity failures.

## Capabilities

### Modified Capabilities

- `cli`: startup config behavior for all commands using `cliState.load()` (`serve`, `keys`, `sign`, `health`)

## Impact

- **Behavior change**: env-only operation works without creating `~/.kms-wrapper/config.yaml`.
- **Operator UX**: missing config path becomes warning + fallback, not hard failure.
- **No security downgrade**: required secrets are still mandatory at runtime validation.

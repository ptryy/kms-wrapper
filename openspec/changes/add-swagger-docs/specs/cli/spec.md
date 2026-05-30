## ADDED Requirements

### Requirement: Gateway config supports Swagger toggles via env and YAML
The CLI's config loader SHALL recognize two new optional fields under `gateway`:

- `swagger_enabled` (bool, default `true`) — bound to env var `KMS_GATEWAY_SWAGGER_ENABLED`.
- `swagger_auth` (bool, default `false`) — bound to env var `KMS_GATEWAY_SWAGGER_AUTH`.

Both SHALL be readable from a YAML config file, an env var, or fall back to the documented defaults, using the same precedence rules as the existing `gateway.*` fields.

#### Scenario: Defaults applied when unspecified
- **WHEN** neither `gateway.swagger_enabled` nor `gateway.swagger_auth` appears in the config file or env
- **THEN** the loaded config has `swagger_enabled=true` and `swagger_auth=false`

#### Scenario: Env var overrides YAML
- **WHEN** the config YAML sets `gateway.swagger_auth: false` and `KMS_GATEWAY_SWAGGER_AUTH=true` is set in the environment
- **THEN** the loaded config has `swagger_auth=true`

#### Scenario: YAML disables swagger
- **WHEN** `gateway.swagger_enabled: false` appears in the config file and no env override is set
- **THEN** the loaded config has `swagger_enabled=false` and `kms-wrapper serve` starts without registering `/swagger/*` routes

#### Scenario: Invalid boolean value
- **WHEN** `KMS_GATEWAY_SWAGGER_ENABLED=notabool` is set
- **THEN** `kms-wrapper serve` exits non-zero with a config parse error

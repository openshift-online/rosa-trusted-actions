---
type: reference
title: "Operations: Configuration Reference"
description: Reference for every CLI flag, environment variable, and config-file key the ROSA Trusted Actions server reads, including precedence rules and the fail-fast guards enforced at startup.
tags: [configuration, operations, environment-variables, cli-flags, yaml, startup, authorization, aws, kubernetes]
verified:
  - by: openwiki/0.7.1
    at: 2026-11-05T16:04:34.033Z
sources:
  - id: openwiki-source-0f6ed11169bd8e6b34b44280
    resource: repo://cmd/server/main.go
  - id: openwiki-source-706925cc8c84991497d2f1ad
    resource: repo://config/config.yaml
  - id: openwiki-source-7fc34fb47598e8f8a8294c1f
    resource: repo://internal/authorization/authorization.go
  - id: openwiki-source-ac908664ab805ed983c75cf1
    resource: repo://internal/authorization/aws.go
  - id: openwiki-source-a8910515ddd14810ad43f5c1
    resource: repo://internal/config/config.go
generated: { by: "openwiki/0.7.1", at: "2026-11-05T16:04:34.033Z" }
---

# Operations: Configuration Reference

The server (`cmd/server/main.go`) is configured from three sources, combined by
[`config.Load`](../concepts/authorization-model.md) (`internal/config/config.go`):

1. **CLI flags** (via `cobra`) — server-level settings only: `--config-file`, `--listen-addr`,
   `--log-level`, `--log-json`.
2. **Environment variables**, all but a handful prefixed `ROSA_TA_`.
3. **The YAML config file** passed via `--config-file` — a narrower set of keys covering
   workers, action allowlists, and the AWS/backplane connection settings.

Where more than one source can set the same value, **CLI flags win over environment
variables, and environment variables win over the config file**, which in turn wins over
built-in defaults. `cmd/server/main.go` applies CLI flags only when the flag was actually
passed (`cmd.Flags().Changed(...)`), so an unset flag never clobbers an environment value.

This page is a reference. For *why* the two allowlist families have opposite fail-safe
defaults, and for the full authentication/authorization pipeline, see
[Authorization Model](../concepts/authorization-model.md). For how these settings are
exercised when running or deploying the server, see
[Local Development](local-development.md) and [Deployment](deployment.md).

## `config.Config` sections

The `Config` struct (`internal/config/config.go`) groups settings into the sections below.
"Source" lists every place a value can come from; env always takes precedence over the
config file when both are listed.

### Server

| Field | CLI flag | Env var | Default |
|---|---|---|---|
| `ListenAddr` | `--listen-addr` | `ROSA_TA_LISTEN_ADDR` | `:8080` |
| `LogLevel` | `--log-level` | `ROSA_TA_LOG_LEVEL` | `info` |
| `LogJSON` | `--log-json` | `ROSA_TA_LOG_JSON` | `false` |
| — | `--config-file` | — | `""` (no config file) |

### AWS (connection/credentials)

| Field | Env var | Config file key | Default | Notes |
|---|---|---|---|---|
| `AWSRegion` | `AWS_REGION` | — | `""` | Standard AWS SDK variable |
| `AWSAccessKeyID` | `AWS_ACCESS_KEY_ID` | — | `""` | Used only by the static AWS client provider |
| `AWSSecretAccessKey` | `AWS_SECRET_ACCESS_KEY` | — | `""` | Used only by the static AWS client provider |
| `AWSSessionToken` | `AWS_SESSION_TOKEN` | — | `""` | Used only by the static AWS client provider |
| `AWSEndpointURL` | `ROSA_TA_AWS_ENDPOINT_URL` | `aws.endpoint_url` | `""` | Points AWS traffic at a local emulator (e.g. LocalStack). Setting it also selects the static AWS client provider instead of the backplane-credentials provider — a single fixed endpoint can only mean a single emulated account |
| `AWSProxyURL` | `ROSA_TA_AWS_PROXY_URL` | `aws.proxy_url` | `""` (falls back to `BackplaneProxyURL`) | Proxy for AWS traffic |

### Storage

| Field | Env var | Default |
|---|---|---|
| `S3Bucket` | `ROSA_TA_S3_BUCKET` | `""` |
| `S3KeyPrefix` | `ROSA_TA_S3_KEY_PREFIX` | `"trusted-actions"` |

### Security

| Field | Env var / config | Default | Notes |
|---|---|---|---|
| `AllowedAccounts` | `ROSA_TA_ALLOWED_ACCOUNTS` (comma-separated) | `nil` | AWS account IDs the server will operate against |
| `Roles` | config file `roles:` key only | `nil` (empty) | List of `{id, amsResource}` mappings; `ROSA_TA_ROLES_CONFIG` is no longer read and only triggers a startup warning if set |
| `JWKCertFile` | `ROSA_TA_JWK_CERT_FILE` | `""` | Optional local JWKS file |
| `JWKCertURL` | `ROSA_TA_JWK_CERT_URL` | `https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/certs` | JWKS endpoint used to verify OCM JWTs |

### OCM

| Field | Env var | Default |
|---|---|---|
| `OCMBaseURL` | `ROSA_TA_OCM_BASE_URL` | `https://api.openshift.com` |
| `OCMClientID` | `ROSA_TA_OCM_CLIENT_ID` | `""` |
| `OCMClientSecret` | `ROSA_TA_OCM_CLIENT_SECRET` | `""` |
| `OCMToken` | `ROSA_TA_OCM_TOKEN` | `""` |

These are only used to build the OCM client when `ROSA_TA_AUTH=enabled` (see
[Auth policies](#three-rosa_ta_auth-policies) below).

### Database

| Field | Env var | Default |
|---|---|---|
| `DatabaseURL` | `DATABASE_URL` | `""` |
| `DBMaxOpenConns` | `ROSA_TA_DB_MAX_OPEN_CONNS` | `10` (falls back to default if not a positive integer) |
| `DBMaxIdleConns` | `ROSA_TA_DB_MAX_IDLE_CONNS` | `5` (falls back to default if not a positive integer) |
| `DBConnMaxLifetime` | `ROSA_TA_DB_CONN_MAX_LIFETIME` | `5m` (falls back to default if not a positive duration) |

### Workers

| Field | Env var | Config file key | Default |
|---|---|---|---|
| `WorkerConcurrency` | `ROSA_TA_WORKER_CONCURRENCY` | `workers.concurrency` | `4` |
| `WorkerPollInterval` | `ROSA_TA_WORKER_POLL_INTERVAL` | `workers.poll_interval` | `5s` |
| `WorkerExecutionTimeout` | `ROSA_TA_WORKER_EXECUTION_TIMEOUT` | `workers.execution_timeout` | `2m` |

Env vars take precedence over the config file for all three; `readConfigFile` seeds
`4` / `5s` / `2m` onto the in-memory `configFile` struct before parsing, so a config file
that omits `workers:` entirely still yields those same values. Non-positive values from
either source are rejected in favor of the default (`getPositiveIntEnv` /
`getPositiveDurationEnv`), since `time.NewTicker` panics on a non-positive duration.

### Backplane

| Field | Env var | Config file key | Default | Notes |
|---|---|---|---|---|
| `BackplaneURL` | `ROSA_TA_BACKPLANE_URL` | — | `""` | Shared by the HMAC-signed trusted-action endpoint (`internal/backplane`) and backplane-cli's cloud-credential flow (`awsapi.CloudCredentialsProvider`); must use `https://` or the server fails to start |
| `BackplaneClientID` | `ROSA_TA_BACKPLANE_CLIENT_ID` | — | `""` | |
| `BackplaneClientSecret` | `ROSA_TA_BACKPLANE_CLIENT_SECRET` | — | `""` | |
| `BackplaneAssumeInitialARN` | `ROSA_TA_BACKPLANE_ASSUME_INITIAL_ARN` | `backplane.assume_initial_arn` | `""` | Overrides the payer-account role the AWS assume-role chain starts from; normally left empty so backplane-cli infers it from the OCM URL |
| `BackplaneProxyURL` | `ROSA_TA_BACKPLANE_PROXY_URL` | `backplane.proxy_url` | `""` | Proxy used to reach the backplane API; also the fallback for `AWSProxyURL` |

### Kubeconfig

| Field | Env var | Default | Notes |
|---|---|---|---|
| `Kubeconfig` | `ROSA_TA_KUBECONFIG` | `""` | Local testing only. When set, selects `backplane.NewKubeconfigProvider` instead of the real backplane provider for Kubernetes access |

### Actions

| Field | Env var | Config file key | Default |
|---|---|---|---|
| `AllowedNamespaces` | `ROSA_TA_ALLOWED_NAMESPACES` (comma-separated) | `actions.allowed_namespaces` | `nil` (empty — permissive) |
| `AllowedSecrets` | `ROSA_TA_ALLOWED_SECRETS` (comma-separated `namespace/name`) | `actions.allowed_secrets` | `nil` (empty) |
| `AWSAllowedRegions` | `ROSA_TA_AWS_ALLOWED_REGIONS` (comma-separated) | `actions.aws_allowed_regions` | `authorization.DefaultAllowedRegions` (empty — deny everything) |
| `AWSAllowedOperations` | `ROSA_TA_AWS_ALLOWED_OPERATIONS` (comma-separated) | `actions.aws_allowed_operations` | `nil` (empty — no writes) |

Env vars take precedence over the config file for all four. See
[The two allowlists](#the-two-allowlists-and-their-fail-safe-defaults) below for the
behavioral consequences of each default.

### Development / local-testing

| Field | Env var | Default | Notes |
|---|---|---|---|
| `AuthPolicy` | `ROSA_TA_AUTH` (preferred), or legacy boolean `ROSA_TA_ENABLE_AUTH` | `enabled` | See [Three `ROSA_TA_AUTH` policies](#three-rosa_ta_auth-policies) |

If `ROSA_TA_AUTH` is set to anything other than `enabled`, `ocmconfig`, or `disabled`,
`config.Load` returns an error and the server exits immediately (not a `logger.Fatal`,
since logging is not yet configured at that point). If `ROSA_TA_AUTH` is unset,
`ROSA_TA_ENABLE_AUTH=false` is a legacy way to select `disabled`; otherwise the default is
`enabled`.

## The two allowlists and their fail-safe defaults

Two allowlist families gate what a running action may touch, and they fail safe in
opposite directions:

- **`ROSA_TA_ALLOWED_NAMESPACES`** / `actions.allowed_namespaces` — **permissive when
  unset.** An empty list makes the Kubernetes resource authorizer (`authorization.New`)
  allow every namespaced request. This is convenient for local development and
  cluster-scoped-only deployments, but it means **production deployments must set this
  explicitly** to actually restrict which namespaces actions can target. `ROSA_TA_ALLOWED_SECRETS`
  / `actions.allowed_secrets` is a second, independent allowlist layered on top for
  `secrets`-typed resources specifically; it has the same empty-means-nothing-extra
  behavior (no secrets requests pass unless explicitly listed by `namespace/name`), but it
  does not widen access when `ROSA_TA_ALLOWED_NAMESPACES` is itself empty.

- **`ROSA_TA_AWS_ALLOWED_REGIONS`** / `actions.aws_allowed_regions` — **deny-everything
  when unset.** `authorization.DefaultAllowedRegions` is an empty slice by design, not a
  placeholder, and `config.Load` falls back to it whenever neither the env var nor the
  config file name a region. An empty region allowlist makes the AWS authorizer
  (`authorization.NewAWS`) refuse every AWS request. **`cmd/server/main.go` goes one step
  further and refuses to start at all** if `len(cfg.AWSAllowedRegions) == 0` after
  `config.Load` runs (`logger.Fatal`), because a deployment that would otherwise come up
  healthy and deny every AWS execution should say so at startup, not on the first
  AWS-backed request. This setting has **no built-in non-empty default** — it must be set
  explicitly by every deployment that uses AWS actions.

- **`ROSA_TA_AWS_ALLOWED_OPERATIONS`** / `actions.aws_allowed_operations` is a second,
  independent AWS gate for *writes* only (e.g. `reboot`, `terminate`); it has no
  non-empty default either, but unlike the region allowlist its empty state is not a
  startup failure — it simply means **reads are permitted in the allowed regions and
  writes are not**. `cmd/server/main.go` logs a warning at startup whenever this list is
  non-empty, since enabling writes is the one AWS policy decision that widens what the
  server can do to a customer's cloud infrastructure.

See [Authorization Model](../concepts/authorization-model.md) for the full rationale and
request-time evaluation order of both authorizers.

## YAML config file schema

Passed via `--config-file path/to/config.yaml` (see `config/config.yaml` for the
checked-in example). All keys are optional; omitted keys fall back to their environment
variable or built-in default, in that order. The file is parsed into `configFile`
(`internal/config/config.go`) via `gopkg.in/yaml.v3`.

```yaml
# Role catalog used only when auth is enabled (see below). No environment
# variable equivalent — this is config-file only.
roles:
  - id: "SREP"
    amsResource: "BackplaneOsdSrepResource"
  - id: "ConfigurationAnomalyDetection"
    amsResource: "BackplaneOsdCadResource"
  - id: "ROSAAiAgent"
    amsResource: "BackplaneOsdAiAgentResource"

workers:
  concurrency: 4          # overridden by ROSA_TA_WORKER_CONCURRENCY
  poll_interval: 5s        # overridden by ROSA_TA_WORKER_POLL_INTERVAL
  execution_timeout: 2m    # overridden by ROSA_TA_WORKER_EXECUTION_TIMEOUT

actions:
  allowed_namespaces: []           # overridden by ROSA_TA_ALLOWED_NAMESPACES
  allowed_secrets: []              # overridden by ROSA_TA_ALLOWED_SECRETS
  aws_allowed_regions: [us-east-1] # overridden by ROSA_TA_AWS_ALLOWED_REGIONS; no default
  aws_allowed_operations: []       # overridden by ROSA_TA_AWS_ALLOWED_OPERATIONS; no default

# Connection settings for the two AWS client providers. Credentials are
# deliberately absent here — this file is typically committed, so
# AWS_ACCESS_KEY_ID and friends stay environment-only.
aws:
  endpoint_url: ""   # overridden by ROSA_TA_AWS_ENDPOINT_URL
  proxy_url: ""      # overridden by ROSA_TA_AWS_PROXY_URL

backplane:
  assume_initial_arn: ""  # overridden by ROSA_TA_BACKPLANE_ASSUME_INITIAL_ARN
  proxy_url: ""           # overridden by ROSA_TA_BACKPLANE_PROXY_URL
```

Notes on the file's behavior:

- `readConfigFile` seeds `workers.concurrency=4`, `workers.poll_interval=5s`, and
  `workers.execution_timeout=2m` onto the struct *before* parsing the file, so these three
  defaults apply even when the file omits the `workers:` block entirely, and even when no
  `--config-file` is passed at all (an empty path skips reading but keeps the seeded
  defaults).
- A missing file at the given path, or a file that fails to parse as YAML, makes
  `config.Load` return an error and the server does not start.
- `roles:` has no environment variable form — it is config-file only. The previously
  supported `ROSA_TA_ROLES_CONFIG` environment variable is no longer read; setting it only
  prints a startup warning.
- `aws:` and `backplane:` in the file configure the two AWS *connection* providers
  (static-vs-backplane selection and proxy/ARN overrides), not credentials, which remain
  environment-only (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`).

## Three `ROSA_TA_AUTH` policies

`cmd/server/main.go` switches on `cfg.AuthPolicy` to decide how to authenticate and
authorize incoming requests, and what each policy requires to start successfully:

| Policy | Requires | Builds | Fails to start if |
|---|---|---|---|
| `enabled` (default) | Non-empty `roles:` in the config file, each with both `id` and `amsResource` set; OCM client credentials (`ROSA_TA_OCM_CLIENT_ID` / `ROSA_TA_OCM_CLIENT_SECRET` and/or `ROSA_TA_OCM_TOKEN`) | Real `auth.NewAuthMiddleware`, `auth.NewRoleAuthzMiddleware`, and an `ocm.Client` built from `cfg.OCMBaseURL`/`OCMClientID`/`OCMClientSecret`/`OCMToken` | `cfg.Roles` is empty, any role entry has an empty `id` or `amsResource`, or `ocm.NewClient` fails — all via `logger.Fatal` |
| `ocmconfig` | Same non-empty, well-formed `roles:` as `enabled`; a local OCM CLI config (typically from `ocm login`) readable via `ocmconfig.Load()` | Same real auth/authz middleware, but the `ocm.Client` is built from the local OCM config's URL/client ID/secret/access+refresh tokens instead of env vars | Same role-validation checks as `enabled`, plus `ocmconfig.Load()` failing or the resulting `ocm.NewClient` call failing — via `logger.Fatal`. Documented as development-only |
| `disabled` | `ROSA_TA_KUBECONFIG` set (a local kubeconfig path); `ROSA_TA_AWS_ENDPOINT_URL` set (an AWS emulator endpoint) | `auth.NewMockAuthMiddleware()` (hardcoded `dev-user` identity) and `auth.NewMockAuthzMiddleware` (hardcoded `SREP` role); no OCM client is built | Either `cfg.Kubeconfig` or `cfg.AWSEndpointURL` is empty — via `logger.Fatal`, since mock auth against the real backplane or real AWS would let any unauthenticated caller reach production infrastructure as `SREP` |

An unrecognized value in `ROSA_TA_AUTH` is rejected earlier, inside `config.Load` itself,
before any of the above branches run. See
[Authorization Model](../concepts/authorization-model.md) for why these three policies
exist and how the resulting middleware is wired into the request pipeline.

## Other startup-time (fail-fast) guards

Beyond the auth-policy and region-allowlist checks above, `cmd/server/main.go` enforces:

- **`ROSA_TA_BACKPLANE_URL` must be `https://`** when `ROSA_TA_KUBECONFIG` is not set (i.e.
  whenever the real backplane provider, not the local kubeconfig provider, is used for
  Kubernetes access). A non-HTTPS or unparsable URL is a `logger.Fatal`.
- **An AWS client provider must be constructible.** If `ROSA_TA_AWS_ENDPOINT_URL` is empty
  (so the backplane-credentials AWS provider is needed) but no `ocm.Client` was built
  (which can only happen if `ROSA_TA_AUTH=disabled`, and that branch already requires
  `ROSA_TA_AWS_ENDPOINT_URL` to be set), the server logs `Fatal`. The comment in
  `cmd/server/main.go` notes this branch is unreachable given the other guards, and is
  kept only because the invariant spans two distant code paths.
  Additionally, if an `ocm.Client` is required, its `Connection()` must succeed once at
  startup, or the server fails fast rather than failing every AWS request later.

## Cross-references

- [Authorization Model](../concepts/authorization-model.md) — the full authn/authz
  pipeline these settings configure, and the reasoning behind the fail-safe defaults.
- [Architecture Overview](../architecture/overview.md) — where configuration fits in the
  overall server structure.
- [Local Development](local-development.md) — recommended `.env` values for running the
  server and its dependencies locally.
- [Deployment](deployment.md) — how these settings map onto a production deployment.

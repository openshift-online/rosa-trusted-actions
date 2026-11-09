# ROSA Trusted Actions

[![Prow Status](https://prow.ci.openshift.org/badge.svg?jobs=pull-ci-openshift-online-rosa-trusted-actions-main-make-test)](https://prow.ci.openshift.org/badge.svg?jobs=pull-ci-openshift-online-rosa-trusted-actions-main-make-test)

## Overview

Modular, tested, API to interact with ROSA clusters. Based on OpenAPI.

## Prerequisites

- Go 1.21+
- Node.js 16+ (for OpenAPI bundling)
- Make
- Git

## Quick Start

```bash
# Build and run
make build
make run

# The server starts on :8080 by default
```

## Local Development

The server requires:
- OCM credentials to target OCM and backplane services
- A reachable JWKS endpoint to verify creds passed on its endpoints.

> **Warning:** never set `ROSA_TA_AUTH` outside a local or CI environment.

### `ROSA_TA_AUTH=ocmconfig` mode

This is the integrated mode which is very similar to production:
- OCM and backplane services will be called using your OCM creds.
- Creds passed on the server endpoints will be checked

> This mode let you test interactions with OCM and backplane... but **you will have to run backplane locally!**

Indeed the server will target backplane using your OCM creds.
But those creds are unprivileged and are not allowed to target `trustedactions` endpoint of backplane.

#### Log on OCM

```bash
ocm login --use-auth-code --url https://api.stage.openshift.com
```

#### Start `backplane-api` server locally

> **Prerequisite**: install the [`yq`](https://github.com/mikefarah/yq#install) tool or you will have to edit configuration files manually!

```bash
git clone git@gitlab.cee.redhat.com:service/backplane-api.git
cd backplane-api
# Run backplane with your OCM creds, the same creds that TA server will use.
# -> self_access_review OCM unprivileged endpoint will be used in place of access_review privileged endpoint
yq -i ".refresh_token = \"$(ocm token --refresh)\"" configs/ocm.json
# Have all backplane roles allowed to use the trustedactions backplane endpoint
yq -i '.trustedActionsAllowedRoles = [.roles[].id]' configs/roles.yml
# Have your OCM user allowed to use that endpoint
yq -i ".trustedActionsAllowedUsernames = [\"$(ocm whoami | yq -r .username)\"]" configs/roles.yml
# Install backplane certs (you will be prompted for password)
sudo make dev-certs
# Finally start backplane locally
make run-local
```

#### Start the server

In an other shell:

```bash
ROSA_TA_AUTH=ocmconfig \
ROSA_TA_BACKPLANE_URL=https://localhost:8001 \
ROSA_TA_AWS_ALLOWED_REGIONS=us-east-1 \
go run ./cmd/server/ --config-file=config/config.yaml --log-level debug
```

You should see:

```text
WARN[2026-11-06T19:56:55+01:00] DATABASE_URL not set, using local file 'trusted_actions.db' — data will be lost in ephemeral environments
INFO[2026-11-06T19:56:55+01:00] SQLite database initialized
INFO[2026-11-06T19:56:55+01:00] Starting server                               addr=":8080"
```

### `ROSA_TA_AUTH=disabled` mode

This is the standalone mode:
- OCM and backplane services are not called.
- Creds passed on its endpoints are not checked

As backplane services are not called, you have to provide the kubeconfig to use; this is handled by `ROSA_TA_KUBECONFIG` env variable.

> **In this mode you cannot test interactions with OCM or backplane!**

#### Get a kubeconfig (OpenShift managed clusters)

For OSD / ROSA clusters, retrieve the admin kubeconfig via OCM using the
cluster's internal ID:

```bash
INTERNAL_ID="<your-cluster-internal-id>"
ocm get /api/clusters_mgmt/v1/clusters/${INTERNAL_ID}/credentials \
  | jq -r .kubeconfig \
  > /tmp/${INTERNAL_ID}.kubeconfig
```

#### Start the server

```bash
ROSA_TA_AUTH=disabled ROSA_TA_KUBECONFIG=/tmp/${INTERNAL_ID}.kubeconfig go run ./cmd/server/ --log-level debug
```

You should see:

```text
WARN  Auth disabled — using mock identity 'dev-user' with SREP role. Do not use in production.
INFO  Using kubeconfig provider for cluster access (local mode)
INFO  Starting server  addr=:8080
```

### Verify the server is up

> This section and the next ones apply whatever `ROSA_TA_AUTH` mode has been chosen to start the server.

In an other shell, run:

```bash
curl -s http://localhost:8080/health | jq .
```

Expected output:
```json
{"status":"healthy","version":"dev","build_date":"unknown","git_commit":"unknown"}
```

### List the action catalog

In another shell rooted at the `rosa-trusted-actions` clone:

#### Using the `rta` CLI

> If you are running the server with the `ROSA_TA_AUTH=disabled` mode, use `--token local` instead.

```bash
# We need to build the CLI before using it -- This is NOT required for the next runs of the CLI
make build-rta
# Calling the CLI
./bin/rta --server=http://127.0.0.1:8080 --token "$(ocm token)" action list
```

You should see:
```text
NAME                   SCOPE     TYPE   DESCRIPTION
get                    kube-api  read   Get or list Kubernetes resources
patch                  kube-api  write  Patch a Kubernetes resource using JSON merge patch
delete                 kube-api  write  Delete a Kubernetes resource
...
```

#### Using `curl`

> If you are running the server with the `ROSA_TA_AUTH=disabled` mode you don't need to define the `Authorization` header.

```bash
curl -s http://localhost:8080/api/v0/trusted-actions/ -H "Authorization: Bearer $(ocm token)" | jq .
```

You should see:
```json
{
  "items": [
    {
      "description": "Get or list Kubernetes resources",
      "name": "get",
      "scope": "kube-api",
      "type": "read"
    },
    ...
  ]
}
```

### Execute a GET action against a cluster

The `get` action lists or fetches Kubernetes resources. `resource`, `version`, and
`namespace` parameters are all required — nothing is defaulted server-side.

> In below commands: replace `<cluster-id>` with the internal cluster ID of the cluster you intend to target or with `local` if the mode is `ROSA_TA_AUTH=disabled`

#### Using the `rta` CLI

> If you are running the server with the `ROSA_TA_AUTH=disabled` mode, use `--token local` instead.

```bash
# List pods in the default namespace
./bin/rta --server=http://127.0.0.1:8080 --token "$(ocm token)" \
  action run get --cluster <cluster-id> --jira TEST-1234 -o json \
    --param resource=pods \
    --param version=v1 \
    --param namespace=default

# List pods in a different namespace
./bin/rta --server=http://127.0.0.1:8080 --token "$(ocm token)" \
  action run get --cluster <cluster-id> --jira TEST-1234 -o json \
    --param resource=pods \
    --param version=v1 \
    --param namespace=kube-system

# Get a specific pod
./bin/rta --server=http://127.0.0.1:8080 --token "$(ocm token)" \
  action run get --cluster <cluster-id> --jira TEST-1234 -o json \
    --param resource=pods \
    --param version=v1 \
    --param namespace=openshift-monitoring \
    --param name=prometheus-k8s-0
```

> **Remark that the `rta` CLI waits for the execution to run and prints its ouput**

#### Using `curl`

> If you are running the server with the `ROSA_TA_AUTH=disabled` mode you don't need to define the `Authorization` header.

```bash
# List pods in the default namespace
curl -s -X POST http://localhost:8080/api/v0/trusted-actions/get/run \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $(ocm token)" \
  -d '{
    "target_cluster": "<cluster-id>",
    "params": {"resource": "pods", "version": "v1", "namespace": "default"}
  }' | jq

# List pods in a different namespace
curl -s -X POST http://localhost:8080/api/v0/trusted-actions/get/run \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $(ocm token)" \
  -d '{
    "target_cluster": "<cluster-id>",
    "params": {"resource": "pods", "version": "v1", "namespace": "kube-system"}
  }' | jq

# Get a specific pod
curl -s -X POST http://localhost:8080/api/v0/trusted-actions/get/run \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $(ocm token)" \
  -d '{
    "target_cluster": "<cluster-id>",
    "params": {
      "resource":  "pods",
      "version":   "v1",
      "namespace": "openshift-monitoring",
      "name":      "prometheus-k8s-0"
    }
  }' | jq
```

> **Those commands do NOT return the execution ouput.**
>
> Those commands create an execution and queue it for running; the `_links.self.href` field give the endpoint to target to get execution details.

Typical output:
```json
{
  "_links": {
    "self": {
      "href": "/api/v0/trusted-actions/runs/<execution-id>",
      "method": "GET"
    }
  },
  "action": "get",
  "id": "<execution-id>",
  "status": "pending",
  "target_cluster": "<cluster-id>",
  ...
}
```

> Extract the `<execution-id>` id from above output and replace it in next commands

Now lets get the execution details once again:

```bash
curl -s -X GET http://localhost:8080/api/v0/trusted-actions/runs/<execution-id> \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $(ocm token)" | jq
```

Output:
```json
{
  "_links": {
    "output": {
      "href": "/api/v0/trusted-actions/runs/<execution-id>/output",
      "method": "GET"
    },
    "self": {
      "href": "/api/v0/trusted-actions/runs/<execution-id>",
      "method": "GET"
    }
  },
  "action": "get",
  "id": "<execution-id>",
  "status": "succeeded",
  "target_cluster": "<cluster-id>",
  ...
}
```

Remark that:
- The `status` changed from `pending` to `succeeded`.
- The execution does not contain the output itself... but the output can be queried through an other endpoint: `/api/v0/trusted-actions/runs/<execution-id>/output`

Lets finally retrieve the output:

```bash
curl -s -X GET http://localhost:8080/api/v0/trusted-actions/runs/<execution-id>/output \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $(ocm token)" | jq
```
(the only change compared to the last command is `/output` appended to the endpoint)

Typical output:
```json
{
  "_links": {
    "execution": {
      "href": "/api/v0/trusted-actions/runs/<execution-id>",
      "method": "GET"
    },
    "self": {
      "href": "/api/v0/trusted-actions/runs/<execution-id>/output",
      "method": "GET"
    }
  },
  "message": "got pods/prometheus-k8s-0 in openshift-monitoring",
  "resources": [
    {
      "apiVersion": "v1",
      "kind": "Pod",
      ...
    }
  ]
}
```

#### Supported params

| Param       | Required | Description                                                           |
|-------------|----------|------------------------------------------------------------------------|
| `resource`  | yes      | Kubernetes resource type, plural (e.g. `pods`, `configmaps`)          |
| `version`   | yes      | API version (e.g. `v1`)                                               |
| `namespace` | yes      | Namespace to query — every request is currently treated as namespaced |
| `name`      | no       | Specific resource name — omit to list all                             |
| `group`     | no       | API group (omit for core resources)                                   |

## Development

### Code Generation

The OpenAPI specification lives in the `openapi/` directory. The build process bundles the multi-file spec and generates Go server code:

```bash
# Regenerate API code from the local spec
make generate

# Validate the OpenAPI spec
make validate-spec

# Preview API docs in the browser
npm run preview-docs
```

### Building and Testing

```bash
# Build
make build

# Run tests
make test

# Integration tests
make test-integration

# Code quality
make lint
make fmt
```

## Configuration

### CLI Flags (Server Settings)

```bash
./bin/rosa-trusted-actions-server --listen-addr=":3000" --log-level="debug" --log-json
```

### Config File

Use `--config-file` to pass a YAML config file with workers, actions, and role mappings.
See [`config.yaml`](./config/config.yaml) for the full schema.

When auth is enabled (the default), the `roles` key is required.

### Environment Variables (Application Config)

```bash
# AWS Configuration
export AWS_REGION="us-east-1"
export AWS_ACCESS_KEY_ID="your-key"
export AWS_SECRET_ACCESS_KEY="your-secret"

# Application Configuration
export ROSA_TA_ALLOWED_ACCOUNTS="123456789012,987654321098"

# Security Configuration
export ROSA_TA_JWK_CERT_FILE=""  # optional
export ROSA_TA_JWK_CERT_URL="https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/certs"
# OCM Client Configuration
export ROSA_TA_OCM_BASE_URL="https://api.openshift.com"
export ROSA_TA_OCM_CLIENT_ID="some-client-id"
export ROSA_TA_OCM_CLIENT_SECRET="xxxxxxxxxxxxxxx"
export ROSA_TA_OCM_TOKEN=""

# Workers Configuration
export ROSA_TA_WORKER_CONCURRENCY="4"
export ROSA_TA_WORKER_POLL_INTERVAL="5s"
export ROSA_TA_WORKER_EXECUTION_TIMEOUT="2m"

# Actions Configuration
export ROSA_TA_ALLOWED_NAMESPACES="openshift-logging,openshift-monitoring,openshift-operators"
export ROSA_TA_ALLOWED_SECRETS="openshift-logging/collector-config,openshift-monitoring/alertmanager-config"
```

Copy `.env.example` to `.env` and customize.
Some of those parameters can also be defined [in a configuration file](./ARCHITECTURE.md#configuration-file).

## API Endpoints

Base path: `/api/v0/trusted-actions`

- `GET /` - List available Trusted Actions
- `GET /{action}` - Describe a specific action
- `POST /{action}/run` - Execute a Trusted Action
- `GET /runs` - List executions
- `GET /runs/{id}` - Get execution details
- `GET /runs/{id}/output` - Get execution output
- `GET /audit` - API call audit log
- `GET /health` - Health check

To add a new Trusted Action, see
[docs/adding-a-trusted-action.md](./docs/adding-a-trusted-action.md) (or the
[`add-trusted-action`](./.claude/skills/add-trusted-action/SKILL.md) Claude
Code skill).

## Project Structure

See [ARCHITECTURE](./ARCHITECTURE.md).

### Show the API in a browsable format

If you want to see the current API as a html package you can use [redocly](https://github.com/redocly/redoc) to render
it into a static HTML page:

``` bash
npx @redocly/cli build-docs api-spec.yaml -o api-spec.html
```

You can now open `api-spec.html` to see the current API.

**DISCLAIMER:** the API is still in development and subject to significant changes.

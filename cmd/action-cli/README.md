# action-cli

Test CLI for executing primitive actions (GET, PATCH, DELETE) against a Kubernetes cluster. It wires up the full executor pipeline — authorization, audit logging, backplane client, and action execution — so you can verify behavior end-to-end without running the server.

## Build

```bash
go build -o action-cli ./cmd/action-cli
```

## Usage

```bash
action-cli run [flags]
```

### Required Flags

| Flag | Description |
|------|-------------|
| `--action` | Action to execute: `get`, `patch`, `delete` |
| `--resource` | Resource type (plural, e.g. `configmaps`, `secrets`, `pods`) |

### Optional Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--kubeconfig` | `$KUBECONFIG` | Path to kubeconfig file |
| `--namespace` | *(empty)* | Target namespace (required for namespaced resources) |
| `--cluster-scoped` | `false` | Target a cluster-scoped resource (e.g. nodes, namespaces) |
| `--name` | *(empty)* | Resource name (omit to list all) |
| `--group` | *(empty)* | API group (empty for core resources) |
| `--version` | `v1` | API version |
| `--patch` | *(empty)* | JSON merge patch body (required for `patch` action) |
| `--cluster-id` | `local` | Cluster identifier for audit records |
| `--caller-id` | `cli-user` | Caller identity for audit records |
| `--cluster-version` | *(empty)* | Cluster OpenShift version |
| `--allowed-namespaces` | *(--namespace)* | Comma-separated namespace allowlist |
| `--allowed-secrets` | *(empty)* | Comma-separated secret allowlist (`namespace/name`) |

## Examples

### List configmaps in a namespace

```bash
action-cli run \
  --kubeconfig ~/.kube/config \
  --action get \
  --namespace openshift-monitoring \
  --resource configmaps
```

### Get a specific configmap

```bash
action-cli run \
  --kubeconfig ~/.kube/config \
  --action get \
  --namespace openshift-monitoring \
  --resource configmaps \
  --name cluster-monitoring-config
```

### Patch a configmap

```bash
action-cli run \
  --kubeconfig ~/.kube/config \
  --action patch \
  --namespace openshift-monitoring \
  --resource configmaps \
  --name cluster-monitoring-config \
  --patch '{"data":{"key":"value"}}'
```

### Delete a configmap

```bash
action-cli run \
  --kubeconfig ~/.kube/config \
  --action delete \
  --namespace openshift-monitoring \
  --resource configmaps \
  --name my-configmap
```

### List namespaces (cluster-scoped)

```bash
action-cli run \
  --kubeconfig ~/.kube/config \
  --action get \
  --resource namespaces \
  --cluster-scoped
```

### Get a specific node (cluster-scoped)

```bash
action-cli run \
  --kubeconfig ~/.kube/config \
  --action get \
  --resource nodes \
  --name ip-10-0-1-100.ec2.internal \
  --cluster-scoped
```

### Get an allowed secret

Secrets are denied by default. Use `--allowed-secrets` to allowlist specific ones:

```bash
action-cli run \
  --kubeconfig ~/.kube/config \
  --action get \
  --namespace openshift-monitoring \
  --resource secrets \
  --name my-secret \
  --allowed-secrets "openshift-monitoring/my-secret"
```

## Authorization

The CLI enforces the same authorization rules as the server:

- **Namespace scoping**: Only namespaces in the allowlist are permitted. If `--allowed-namespaces` is not set, it defaults to the value of `--namespace`.
- **Cluster-scoped resources**: Pass `--cluster-scoped` to target cluster-scoped resources (e.g. `namespaces`, `nodes`, `clusterroles`). Omitting both `--namespace` and `--cluster-scoped` is rejected.
- **Secret deny-by-default**: Access to `secrets` is denied unless the specific `namespace/name` pair appears in `--allowed-secrets`.

Denied requests are logged via the audit logger and the CLI exits with an error.

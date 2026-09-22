# Proxy Backplane Server

A lightweight reverse-proxy that stands in for the real backplane API during local development and
integration testing. Unlike the Imposter mock in `tests/mock-backplane/` — which serves canned
fixture data — this server accepts a real kubeconfig at registration time and proxies every
subsequent request through to the live cluster. The fixture mock is useful for fast, deterministic
tests; this proxy is useful when you need to hit a real Kubernetes API.

## How it works

1. **Register** a trusted action by POSTing a base64-encoded kubeconfig. The server parses TLS
   credentials from the kubeconfig, builds an `http.Transport`, and stores the entry in memory.
2. **Proxy** requests through the returned `proxyUri`. The server strips the proxy prefix and
   forwards the request to the upstream cluster, injecting the bearer token from the kubeconfig.
3. **Delete** the action when done. The entry is removed from the in-memory store; the upstream
   cluster is not contacted.

## Run

```bash
make proxy-backplane
```

Or directly:

```bash
go run ./tests/proxy-backplane
```

The server listens on `:8080` by default. To use a different port:

```bash
make proxy-backplane PROXY_BACKPLANE_PORT=9090
# or
go run ./tests/proxy-backplane --listen-addr :9090
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--listen-addr` | `:8080` | Listen address |
| `--log-level` | `info` | Log level (`debug`, `info`, `warn`, `error`) |

## Example: register-then-proxy workflow

Encode a kubeconfig as base64, register a trusted action, then proxy a request to the real cluster:

```bash
# 1. Base64-encode your kubeconfig
KC=$(base64 < ~/.kube/config)

# 2. Register a trusted action
CLUSTER_ID=my-cluster-id
RESP=$(curl -s -X POST http://localhost:8080/backplane/trustedactions/$CLUSTER_ID \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"my-action\",\"kubeconfig\":\"$KC\"}")

echo "$RESP" | jq .

# 3. Extract the proxy URI
PROXY_URI=$(echo "$RESP" | jq -r .proxyUri)
INSTANCE_ID=$(echo "$RESP" | jq -r .instanceId)

# 4. Proxy a request to the real cluster (proxyUri is already absolute)
curl -s $PROXY_URI/api/v1/namespaces | jq .

# 5. Check action status
curl -s http://localhost:8080/backplane/trustedactions/$CLUSTER_ID/$INSTANCE_ID | jq .

# 6. Delete the action
curl -s -X DELETE http://localhost:8080/backplane/trustedactions/$CLUSTER_ID/$INSTANCE_ID
```

## URL scheme

The CRUD endpoints use the plural prefix (`/backplane/trustedactions/`) while the proxy uses the
singular form (`/backplane/trustedaction/`). This matches the backplane contract; the asymmetry is
not a typo.

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/backplane/trustedactions/{cluster_id}` | Register an action |
| GET | `/backplane/trustedactions/{cluster_id}/{instanceId}` | Get action status |
| DELETE | `/backplane/trustedactions/{cluster_id}/{instanceId}` | Delete an action |
| * | `/backplane/trustedaction/{cluster_id}/{instanceId}/*` | Reverse proxy to cluster |

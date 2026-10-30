# Local end-to-end testing against a real cluster

This recipe stands up a disposable Kubernetes cluster and drives a new action
through the real server — HTTP → executor → real cluster → response — exercising
real kubeconfig-backed cluster execution. With `ROSA_TA_AUTH=disabled`,
authentication and authorization are mocked and the audit logger is a no-op mock
(`audit.NewMockLogger`); this does not exercise production JWT validation, role
authorization, or persistent audit. It's a lighter-weight alternative to
`integration/itest-up.sh` (skip that script unless the action needs localstack or
the Prometheus/Alertmanager fixtures it sets up for `list-alerts`).

This catches wiring mistakes (catalog/implementation mismatch, bad param
handling, wrong output shape) but **not** RBAC correctness: the local
`KubeconfigProvider` ignores `RequiredRBAC` entirely, so an action with
incorrect or overly broad RBAC will still pass here. Verify RBAC via the unit
tests and code review instead.

Requires `make`, Node.js/`npm` (used by `make build` to bundle the OpenAPI spec),
`kind`, `kubectl`, `go`, `curl`, and `jq` on `PATH`. Works with either `docker`
or `podman` (set `KIND_EXPERIMENTAL_PROVIDER=podman` for the latter).

## 1. Create a disposable cluster

```bash
kind create cluster --name rosa-ta-test --wait 120s --kubeconfig /tmp/rosa-ta-test.kubeconfig
```

Reuse an existing `kindest/node` image if one is already pulled — check with
`podman images | grep kindest` / `docker images | grep kindest` first, note the
cached tag, and pass it explicitly (e.g. `--image kindest/node:v1.33.1`) to
avoid a slow pull. Without `--image`, `kind` pins its own default tag and pulls
it regardless of what's already cached under a different tag.

## 2. Build and start the server with mock auth

```bash
make build

export ROSA_TA_AUTH=disabled
export ROSA_TA_KUBECONFIG=/tmp/rosa-ta-test.kubeconfig
export ROSA_TA_AWS_ENDPOINT_URL=http://127.0.0.1:4566   # loopback LocalStack; never real AWS
export ROSA_TA_AWS_ALLOWED_REGIONS=us-east-1            # required at startup; no default
export DATABASE_URL=/tmp/rosa-ta-test.db

nohup ./bin/rosa-trusted-actions-server --listen-addr 127.0.0.1:8080 --log-level debug > /tmp/rosa-ta-test-server.log 2>&1 &
SERVER_PID=$!
disown
sleep 2
cat /tmp/rosa-ta-test-server.log   # confirm "Starting server" with no fatal errors
```

Keep `$SERVER_PID` around (don't start a new shell) — it's used for teardown in
step 6.

`ROSA_TA_AUTH=disabled` injects a hardcoded `dev-user` identity with the SREP
role — never set this outside local/CI. With auth disabled the server also
requires `ROSA_TA_AWS_ENDPOINT_URL` (a guard against reaching real AWS); point it
at a loopback address even for Kubernetes-only actions. `ROSA_TA_AWS_ALLOWED_REGIONS`
is required unconditionally (there's no default region allowlist) — the server
exits at startup without it even when the action under test never touches AWS.
The default listen address (`:8080`) binds all interfaces;
`--listen-addr 127.0.0.1:8080` keeps this unauthenticated, kubeconfig-backed
server off the network.

## 3. Confirm the action is wired up

```bash
curl -s http://localhost:8080/api/v0/trusted-actions/ | jq .                 # catalog lists it
curl -s http://localhost:8080/api/v0/trusted-actions/<action-name> | jq .    # describe shows params
```

## 4. Run it for real and poll to completion

```bash
RESP=$(curl -s -X POST http://localhost:8080/api/v0/trusted-actions/<action-name>/run \
  -H 'Content-Type: application/json' \
  -d '{"target_cluster":"local","params":{"<param>":"<value>"}}')
EXEC_ID=$(echo "$RESP" | jq -r .id)

# Poll until the run reaches a terminal status; the output endpoint 404s until
# the worker finishes, so don't fetch it before then.
for _ in $(seq 1 30); do
  STATUS=$(curl -s http://localhost:8080/api/v0/trusted-actions/runs/$EXEC_ID | jq -r .status)
  echo "status: $STATUS"
  case "$STATUS" in succeeded|failed|timed_out) break ;; esac
  sleep 1
done

curl -s http://localhost:8080/api/v0/trusted-actions/runs/$EXEC_ID/output | jq .
```

Cross-check the output against the cluster directly where possible (e.g.
`kubectl --kubeconfig /tmp/rosa-ta-test.kubeconfig get <resource> -n <namespace>`)
rather than just trusting that `status: succeeded` means the output is correct.

## 5. Optional: verify the "both registrations are mandatory" failure modes

Useful once, when first wiring up a new action, to build confidence the
catalog/implementation split is wired correctly:

- Comment out the action's `init()` registration, rebuild, restart the server,
  and confirm `POST /{action}/run` still returns `202` but the run ends up
  `status: failed` with `unknown action "<name>"` in the server log.
- Restore it, then remove the catalog entry instead, rebuild, restart, and
  confirm both `GET /{action}` and `POST /{action}/run` now return `404`.
- Restore the catalog entry before moving on.

## 6. Clean up

```bash
kill "$SERVER_PID" 2>/dev/null
kind delete cluster --name rosa-ta-test
rm -f /tmp/rosa-ta-test.kubeconfig /tmp/rosa-ta-test.db /tmp/rosa-ta-test-server.log
```

Don't leave the throwaway action/catalog entry in the tree if it was only created
to validate this recipe — `git status` should come back clean (or show only the
real action being added) before committing.

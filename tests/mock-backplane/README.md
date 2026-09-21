# Mock Server for Trusted Actions

## Local testing

### Install on mac

```bash
brew tap imposter-project/imposter
brew trust imposter-project/imposter
brew install imposter
```

### Install on linux

```bash
curl -L https://raw.githubusercontent.com/imposter-project/imposter-cli/main/install/install_imposter.sh | bash -
```

## Run

```bash
make mock-server
```

This pins the imposter Go engine (v5, native) and disables config-directory auto-restart. State is
held in memory, so restarting resets the mock and nothing leaks between runs.

It listens on 8080, the same port as `make run`. To run both, give the mock another port:

```bash
make mock-server MOCK_SERVER_PORT=8081
```

`kubectl` is not a valid client against this mock: it performs API discovery at startup (requests
to `/api`, `/apis`, and `/api/v1`) and those paths are not modelled. Use `curl` for one-off
requests, or construct a dynamic client with a `rest.Config{Host: proxyUri}` that targets an
explicit GroupVersionResource — the client skips discovery when the GVR is provided directly.

## Example curls

The CRUD endpoints use the plural prefix (`/backplane/trustedactions/`) while the proxy uses the
singular form (`/backplane/trustedaction/`). This matches the backplane contract; the asymmetry is
not a typo.

```bash
curl -X POST http://localhost:8080/backplane/trustedactions/00000000-0000-0000-0000-000000000001 \
  -H "Authorization: Bearer $BACKPLANE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"my-trusted-action","customerDataAccess":false,"rbac":{"clusterRoleRules":[],"roles":[]}}'
```

Instance identifiers are minted per request, so take the `instanceId` from the POST response above
and use it to delete:

```bash
curl -X DELETE http://localhost:8080/backplane/trustedactions/00000000-0000-0000-0000-000000000001/$INSTANCE_ID
```

## Driving the proxy

Append a Kubernetes path to the `proxyUri` the POST returned and the mock answers from the cluster
fixture, refusing anything the trusted action did not request:

```bash
C1=00000000-0000-0000-0000-000000000001
P=$(curl -s -X POST http://localhost:8080/backplane/trustedactions/$C1 \
      -H 'Content-Type: application/json' \
      -d '{"name":"demo","customerDataAccess":false,"rbac":{"clusterRoleRules":[],
           "roles":[{"namespace":"ns-a","rules":[
             {"verbs":["get","list"],"apiGroups":[""],"resources":["pods"]}]}]}}' | jq -r .proxyUri)

curl -s localhost:8080$P/api/v1/namespaces/ns-a/pods        # 200, list envelope
curl -s localhost:8080$P/api/v1/namespaces/ns-a/pods/pod-1  # 200, the object
curl -s localhost:8080$P/api/v1/namespaces/ns-a/pods/gone   # 404, a Kubernetes Status
curl -s localhost:8080$P/api/v1/namespaces/ns-a/configmaps  # 501, no fixture entry
curl -s localhost:8080$P/api/v1/namespaces/ns-a/secrets     # 403, never requested
```

501 and 403 are deliberately different: a gap in the mock must not be able to pass for a legitimate
not-found or for a passing negative test.

A `rest.Config{Host: proxyUri}` drives the same endpoints from an ordinary dynamic client. Expiry is
not enforced, so nothing here depends on the wall clock.

## The cluster fixture

`data/cluster.json` is preloaded into the `cluster` store and describes the clusters the proxy
serves. Keys are `{clusterId}:{kubernetes path}`; each entry declares what it holds and the RBAC
identity of the resource — API group, resource, namespace — which is what access is decided
against:

```json
{
  "<clusterId>:/api/v1/namespaces/ns-a/pods": {
    "kind": "Pod",
    "apiVersion": "v1",
    "identity": { "apiGroup": "", "resource": "pods", "namespace": "ns-a" },
    "items": [ { "apiVersion": "v1", "kind": "Pod", "metadata": { "name": "pod-1" } } ]
  }
}
```

- `items` makes the entry a collection: write only the items and the mock synthesises the list
  envelope, named from `kind`.
- `object` makes it an individual resource, served as written; `apiVersion` and `kind` are filled
  from the entry if the object omits them.
- `"object": null` means the resource is genuinely absent — a deliberate 404, distinct from a path
  with no entry at all.
- an omitted `namespace` means the resource is cluster-scoped, like `/api/v1/nodes`, or is a
  collection read across all namespaces, like `/api/v1/pods`.

The `identity` block is what the resource *is* — it is not a ceiling on what may be granted. The
only rules that decide access are the ones the trusted action requested.

Reads are what the fixture models: a permitted write answers 501 rather than pretending to have
written something. A permitted read of an entry that declares neither `items` nor `object` is 501
too — a malformed entry must not be able to pass for a not-found.

## Cross-cluster isolation

Two clusters are preloaded: C1 (`00000000-0000-0000-0000-000000000001`) holding pods in `ns-a`,
and C2 (`00000000-0000-0000-0000-000000000002`) holding pods in `ns-b`. The composite-key design
(`{clusterId}:{path}` in the fixture store, `{clusterId}:{instanceId}` in the action store) means
no code change was needed to add the second cluster — the isolation falls out of the key structure.

```bash
C1=00000000-0000-0000-0000-000000000001
C2=00000000-0000-0000-0000-000000000002

# Baseline: C1 action granted ns-a pods — confirms C1 serves its own resources.
P1=$(curl -s -X POST http://localhost:8080/backplane/trustedactions/$C1 \
      -H 'Content-Type: application/json' \
      -d '{"name":"c1-baseline","customerDataAccess":false,"rbac":{"clusterRoleRules":[],
           "roles":[{"namespace":"ns-a","rules":[
             {"verbs":["get","list"],"apiGroups":[""],"resources":["pods"]}]}]}}' | jq -r .proxyUri)

curl -s localhost:8080$P1/api/v1/namespaces/ns-a/pods/pod-1  # 200 — C1 has pod-1 in ns-a

# Generous grant: C1 action whose rules cover ns-b pods. The grant is generous; the
# inventory is simply not C1's, so the fixture entry is absent.
P1G=$(curl -s -X POST http://localhost:8080/backplane/trustedactions/$C1 \
       -H 'Content-Type: application/json' \
       -d '{"name":"c1-generous","customerDataAccess":false,"rbac":{"clusterRoleRules":[],
            "roles":[{"namespace":"ns-b","rules":[
              {"verbs":["get","list"],"apiGroups":[""],"resources":["pods"]}]}]}}' | jq -r .proxyUri)

curl -s localhost:8080$P1G/api/v1/namespaces/ns-b/pods/pod-2  # 501, no fixture entry for C1:ns-b

# C2 action granted ns-b pods — confirms C2 serves its own resources.
P2=$(curl -s -X POST http://localhost:8080/backplane/trustedactions/$C2 \
      -H 'Content-Type: application/json' \
      -d '{"name":"c2-action","customerDataAccess":false,"rbac":{"clusterRoleRules":[],
           "roles":[{"namespace":"ns-b","rules":[
             {"verbs":["get","list"],"apiGroups":[""],"resources":["pods"]}]}]}}' | jq -r .proxyUri)

curl -s localhost:8080$P2/api/v1/namespaces/ns-b/pods/pod-2  # 200 — C2 has pod-2 in ns-b

# Cross-cluster: paste C1's instance ID into a C2 proxy URL — refused because the key
# {C2}:{C1-instance} was never written.
C1_INSTANCE=$(echo $P1 | awk -F/ '{print $NF}')
curl -s localhost:8080/backplane/trustedaction/$C2/$C1_INSTANCE/api/v1/namespaces/ns-b/pods/pod-2
# 404, trusted action instance not found for this cluster
```

The 501 on the generous-grant case and the 404 on the cross-cluster probe are deliberately
different: one is a gap in the fixture, the other is a missing action entry.

## What the matcher does, and what it does not

Rules are matched the way Kubernetes matches them: `*` in `verbs`, `apiGroups` or `resources`
matches anything, and the core API group is the empty string. `resourceNames` narrows a rule to the
objects it names, so a grant on `pod-1` can neither read `pod-2` nor list the collection:

```bash
# roles: [{namespace: ns-a, rules: [{verbs:[get], apiGroups:[""], resources:[pods],
#                                    resourceNames:[pod-1]}]}]
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080$P/api/v1/namespaces/ns-a/pods/pod-1  # 200
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080$P/api/v1/namespaces/ns-a/pods/pod-2  # 403
```

A cluster-scoped resource and an all-namespace collection carry no namespace, so only a
`clusterRoleRules` entry authorises them — a `roles` entry is bound to one namespace and cannot.

The matcher is deliberately small, and its gaps are loud rather than silent:

- **Selectors are ignored.** `?labelSelector=` or `?fieldSelector=` on a request is answered
  unfiltered, and the mock logs a warning naming the selector so the result is not mistaken for a
  filtered one.
- **Subresources are 501.** `/api/v1/namespaces/ns-a/pods/pod-1/log` and friends are not modelled.
- **Non-resource URLs are 501.** `/healthz`, `/version` and discovery paths such as `/api/v1` are
  not modelled either.

## Deliberate deviations

Three properties of this mock look like bugs to a first reader; they are not.

**Expiry window is longer than the contract's.** The mock returns an expiry 24 hours from
creation. The contract (`CreateTrustedActionResult.expiry`) says 720 minutes (12 hours). The
longer window keeps the mock usable across a full workday without re-creating actions. The
timestamp is not enforced anywhere in the scripts, so the discrepancy has no practical effect.

**Forbidden-rule policy is a hand-copy.** The list of verb/resource combinations that cause
`createTrustedAction` to return 400 (`scripts/trustedactions-post.js`, `FORBIDDEN_POLICIES`) is
transcribed from backplane's unpublished validation logic. Backplane can add, remove, or tighten
entries without notice; any divergence will be silent and will not be caught without a manual
comparison against backplane's source.

**Spec sync is not automated.** `tests/mock-server/backplane.yaml` (the vendored spec this mock
is built from) and the corresponding spec in the upstream client repository were byte-identical
when this README was written. An automated check that flags divergence when either file changes
is planned but not yet implemented; running `diff` manually is the only way to verify they are
still in sync.

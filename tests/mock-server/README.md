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

## Example curls

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

The `identity` block is what the resource *is* — it is not a ceiling on what may be granted. The
only rules that decide access are the ones the trusted action requested.

Reads are what the fixture models: a permitted write answers 501 rather than pretending to have
written something. A permitted read of an entry that declares neither `items` nor `object` is 501
too — a malformed entry must not be able to pass for a not-found.

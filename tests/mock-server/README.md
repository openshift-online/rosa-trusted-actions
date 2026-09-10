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
  -d '{"name":"my-trusted-action","rbac":{"clusterRoleRules":[],"roles":[]}}'
```

Instance identifiers are minted per request, so take the `instanceId` from the POST response above
and use it to delete:

```bash
curl -X DELETE http://localhost:8080/backplane/trustedactions/00000000-0000-0000-0000-000000000001/$INSTANCE_ID
```

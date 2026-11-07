#!/bin/bash
# Run the end-to-end suite (tests/e2e) locally against a real staging cluster,
# doing what the Prow step does: start the server with staging configuration,
# mint a caller token, and drive the HTTP API.
#
# Usage:
#   scripts/e2e-local.sh                      # all tests
#   scripts/e2e-local.sh TestGet_ListPods     # one test, as a -run pattern
#
# Configuration comes from the environment, optionally loaded from a file
# (.env.e2e by default, override with E2E_ENV_FILE). Sourcing
# scripts/setup-local-staging.sh first also works — anything already exported
# is left alone.
#
# Required:
#   E2E_CLUSTER_ID                 OCM cluster ID of the staging cluster to act on
#   ROSA_TA_OCM_CLIENT_ID/SECRET   service account the server authenticates with
#   ROSA_TA_AWS_ALLOWED_REGIONS    the cluster's region; the server will not
#                                  start without one, and there is no default
#
# Optional:
#   ROSA_TA_BACKPLANE_PROXY_URL    proxy the server reaches backplane through,
#                                  defaulting to HTTPS_PROXY. Staging backplane
#                                  is not reachable directly from most networks
#   RTA_TOKEN                      caller token; minted from the service
#                                  account when unset (e.g. use `ocm token` to
#                                  call as yourself instead)
#   ROSA_TA_BACKPLANE_CLIENT_ID/SECRET   defaults to the OCM pair
#   E2E_SEED_FIXTURES=true         create the namespace, configmap and pod the
#                                  'patch' and 'delete' tests need, using the
#                                  current KUBECONFIG, and remove them after
#   E2E_KEEP_FIXTURES=true         leave seeded fixtures behind
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

RUN_PATTERN="${1:-}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${YELLOW}==>${NC} $*"; }
ok() { echo -e "${GREEN}✓${NC} $*"; }
fail() { echo -e "${RED}✗ $*${NC}" >&2; exit 1; }

ENV_FILE="${E2E_ENV_FILE:-.env.e2e}"
if [ -f "$ENV_FILE" ]; then
    log "Loading $ENV_FILE"
    set -a
    # shellcheck source=/dev/null
    source "$ENV_FILE"
    set +a
fi

SERVER_PORT="${E2E_SERVER_PORT:-8080}"
SERVER_URL="http://localhost:${SERVER_PORT}"
SERVER_LOG="${E2E_SERVER_LOG:-/tmp/rta-e2e-server.log}"
SERVER_BIN=/tmp/rta-e2e-server
CONFIG_FILE="${RTA_CONFIG_FILE:-config/ci.yaml}"
SSO_TOKEN_URL="${SSO_TOKEN_URL:-https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token}"

export ROSA_TA_AUTH=enabled
export ROSA_TA_OCM_BASE_URL="${ROSA_TA_OCM_BASE_URL:-https://api.stage.openshift.com}"
export ROSA_TA_BACKPLANE_URL="${ROSA_TA_BACKPLANE_URL:-https://api.stage.backplane.openshift.com}"
export ROSA_TA_BACKPLANE_CLIENT_ID="${ROSA_TA_BACKPLANE_CLIENT_ID:-${ROSA_TA_OCM_CLIENT_ID:-}}"
export ROSA_TA_BACKPLANE_CLIENT_SECRET="${ROSA_TA_BACKPLANE_CLIENT_SECRET:-${ROSA_TA_OCM_CLIENT_SECRET:-}}"
export DATABASE_URL="${DATABASE_URL:-/tmp/rta-e2e.db}"

for required in E2E_CLUSTER_ID ROSA_TA_OCM_CLIENT_ID ROSA_TA_OCM_CLIENT_SECRET ROSA_TA_AWS_ALLOWED_REGIONS; do
    [ -n "${!required:-}" ] || fail "$required is not set (see the header of this script)"
done

# The same guard the CI step applies. Everything below reaches a real cluster
# with real credentials, so pointing it at production has to be deliberate
# rather than a forgotten default.
case "$ROSA_TA_OCM_BASE_URL" in
    *stage* | *integration* | *int.*) ;;
    *) fail "refusing to run against non-staging OCM: $ROSA_TA_OCM_BASE_URL" ;;
esac
case "$ROSA_TA_BACKPLANE_URL" in
    https://*stage* | https://*int*) ;;
    *) fail "refusing to run against non-staging backplane: $ROSA_TA_BACKPLANE_URL" ;;
esac

# The server selects the kubeconfig provider over backplane whenever this is
# set, which would skip the path the suite exists to exercise.
if [ -n "${ROSA_TA_KUBECONFIG:-}" ]; then
    log "Unsetting ROSA_TA_KUBECONFIG so the server reaches the cluster through backplane"
    unset ROSA_TA_KUBECONFIG
fi

# --- proxy ------------------------------------------------------------------
# Staging backplane is not reachable directly from most networks: the request
# dials its address and times out. Only the server needs the proxy — the suite
# talks to it over plain HTTP on localhost — so it is applied to the server
# process alone, further down.
PROXY_URL="${ROSA_TA_BACKPLANE_PROXY_URL:-${HTTPS_PROXY:-${https_proxy:-}}}"
if [ -z "$PROXY_URL" ]; then
    log "No ROSA_TA_BACKPLANE_PROXY_URL or HTTPS_PROXY set — staging backplane is likely unreachable from this network"
else
    log "The server will reach backplane through $PROXY_URL"

    # An exclusion that matches the backplane host sends the request straight
    # out again, which looks exactly like having configured no proxy at all.
    backplane_host=${ROSA_TA_BACKPLANE_URL#https://}
    backplane_host=${backplane_host%%/*}
    for entry in $(echo "${NO_PROXY:-${no_proxy:-}}" | tr ',' ' '); do
        entry=${entry#.}
        [ -n "$entry" ] || continue
        case "$backplane_host" in
            "$entry" | *".$entry")
                fail "NO_PROXY contains '$entry', which excludes $backplane_host from the proxy; the server would dial it directly and time out"
                ;;
        esac
    done
fi

# --- fixtures ---------------------------------------------------------------
# The writing tests act on seeded objects and nothing else. Created here with
# the caller's own cluster access, the way the CI job creates them in a
# separate step: the server under test must not be the thing that makes them.
FIXTURE_NAMESPACE="${E2E_FIXTURE_NAMESPACE:-rosa-ta-e2e}"
SEEDED=false

cleanup() {
    if [ -n "${SERVER_PID:-}" ]; then
        kill "$SERVER_PID" 2> /dev/null || true
        wait "$SERVER_PID" 2> /dev/null || true
    fi
    if [ "$SEEDED" = true ] && [ "${E2E_KEEP_FIXTURES:-false}" != true ]; then
        log "Removing fixtures from $FIXTURE_NAMESPACE"
        oc delete namespace "$FIXTURE_NAMESPACE" --wait=false > /dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

if [ "${E2E_SEED_FIXTURES:-false}" = true ]; then
    command -v oc > /dev/null 2>&1 || fail "E2E_SEED_FIXTURES=true needs 'oc' on PATH"
    suffix=$(head -c 4 /dev/urandom | od -An -tx1 | tr -d ' \n')
    export E2E_FIXTURE_NAMESPACE="$FIXTURE_NAMESPACE"
    export E2E_FIXTURE_CONFIGMAP="e2e-patch-${suffix}"
    export E2E_FIXTURE_POD="e2e-delete-${suffix}"

    log "Seeding fixtures in $FIXTURE_NAMESPACE as $(oc whoami 2> /dev/null || echo 'unknown user')"
    oc create namespace "$FIXTURE_NAMESPACE" > /dev/null 2>&1 || true
    oc create configmap "$E2E_FIXTURE_CONFIGMAP" -n "$FIXTURE_NAMESPACE" --from-literal=seeded=true > /dev/null
    oc run "$E2E_FIXTURE_POD" -n "$FIXTURE_NAMESPACE" \
        --image="${E2E_FIXTURE_POD_IMAGE:-registry.access.redhat.com/ubi9/ubi-minimal:latest}" \
        --restart=Never --command -- /bin/sleep infinity > /dev/null
    SEEDED=true
    ok "Seeded configmap $E2E_FIXTURE_CONFIGMAP and pod $E2E_FIXTURE_POD"
elif [ -z "${E2E_FIXTURE_CONFIGMAP:-}" ] || [ -z "${E2E_FIXTURE_POD:-}" ]; then
    log "No fixtures configured; the patch and delete tests will skip (set E2E_SEED_FIXTURES=true to create them)"
fi

# An allowlist in the environment overrides the one in the config file, so a
# shell set up for something else silently denies every fixture request.
if [ -n "${ROSA_TA_ALLOWED_NAMESPACES:-}" ] && [ "${E2E_SEED_FIXTURES:-false}" = true ]; then
    case ",${ROSA_TA_ALLOWED_NAMESPACES}," in
        *",${FIXTURE_NAMESPACE},"*) ;;
        *) fail "ROSA_TA_ALLOWED_NAMESPACES is set and does not include ${FIXTURE_NAMESPACE}; the writing tests would be denied" ;;
    esac
fi

# --- caller token -----------------------------------------------------------
# Reused when already exported, so calling the API as yourself is just
# RTA_TOKEN=$(ocm token) in front of this script.
if [ -z "${RTA_TOKEN:-}" ]; then
    log "Requesting an access token for ${ROSA_TA_OCM_CLIENT_ID}"
    token_response=$(curl -sS --fail-with-body -X POST "$SSO_TOKEN_URL" \
        -d grant_type=client_credentials \
        -d "client_id=${ROSA_TA_OCM_CLIENT_ID}" \
        -d "client_secret=${ROSA_TA_OCM_CLIENT_SECRET}") \
        || fail "token request to $SSO_TOKEN_URL failed"

    RTA_TOKEN=$(printf '%s' "$token_response" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
    unset token_response
    [ -n "$RTA_TOKEN" ] || fail "no access_token in the SSO response"
    export RTA_TOKEN
    ok "Token acquired"
else
    log "Using the RTA_TOKEN already in the environment"
fi

# --- server -----------------------------------------------------------------
log "Building the server"
go build -o "$SERVER_BIN" ./cmd/server

log "Starting the server on :${SERVER_PORT} (log: $SERVER_LOG)"
# The proxy is exported here rather than for the whole script so that the
# token request, the module downloads and the suite's own localhost traffic
# are left alone. HTTP_PROXY stays unset for the same reason: the suite
# reaches the server over plain HTTP.
(
    if [ -n "$PROXY_URL" ]; then
        export HTTPS_PROXY="$PROXY_URL"
        export NO_PROXY="localhost,127.0.0.1${NO_PROXY:+,${NO_PROXY}}"
        # Also as configuration, which is where backplane-cli's credential
        # flow reads its proxy from. AWS traffic needs no separate setting:
        # awsapi falls back to the backplane proxy when ROSA_TA_AWS_PROXY_URL
        # is unset, the same way backplane-cli does.
        export ROSA_TA_BACKPLANE_PROXY_URL="$PROXY_URL"
    fi
    exec "$SERVER_BIN" --config-file "$CONFIG_FILE" \
        --listen-addr ":${SERVER_PORT}" \
        --log-level "${RTA_LOG_LEVEL:-debug}"
) > "$SERVER_LOG" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 60); do
    curl -sf "${SERVER_URL}/health" > /dev/null 2>&1 && break
    kill -0 "$SERVER_PID" 2> /dev/null || fail "the server exited early — see $SERVER_LOG"
    sleep 1
done
curl -sf "${SERVER_URL}/health" > /dev/null || fail "the server never became healthy — see $SERVER_LOG"
ok "Server healthy at $SERVER_URL"

# --- suite ------------------------------------------------------------------
export RTA_BASE_URL="$SERVER_URL"

log "Running the suite against cluster $E2E_CLUSTER_ID"
# The API does not report why an execution failed, so point at the log that
# does before the output scrolls past.
echo "    failures are explained in $SERVER_LOG"

if [ -n "$RUN_PATTERN" ]; then
    make test-e2e RUN="$RUN_PATTERN"
else
    make test-e2e
fi

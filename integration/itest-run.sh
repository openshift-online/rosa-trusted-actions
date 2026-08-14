#!/bin/bash
set -e

# Retrieves a kubeconfig from the kind cluster created by itest-up.sh, starts the server
# against it with mock auth, and runs a smoke test 'get' action to confirm the API works
# end-to-end. See integration/README.md Phase 1.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CLUSTER_NAME=${ROSA_TA_KIND_CLUSTER_NAME:-"rosa-ta"}
KUBECONFIG_PATH="$SCRIPT_DIR/.kind-kubeconfig"
DB_PATH="$SCRIPT_DIR/.trusted_actions.db"
SERVER_LOG="$SCRIPT_DIR/.server.log"
SERVER_URL="http://localhost:8080"
API_BASE="$SERVER_URL/api/v0/trusted-actions"
WAIT_TIMEOUT=${ROSA_TA_ITEST_WAIT_TIMEOUT:-60}

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() { echo -e "${YELLOW}==>${NC} $*"; }
ok() { echo -e "${GREEN}✓${NC} $*"; }
fail() { echo -e "${RED}✗ $*${NC}" >&2; exit 1; }

for bin in kind go curl jq; do
    command -v "$bin" > /dev/null 2>&1 || fail "'$bin' is required but not found on PATH"
done

# --- 1. Retrieve a kubeconfig from kind ---
kind get clusters 2> /dev/null | grep -qx "$CLUSTER_NAME" \
    || fail "kind cluster '$CLUSTER_NAME' not found — run 'make itest-up' first"
kind get kubeconfig --name "$CLUSTER_NAME" > "$KUBECONFIG_PATH"
ok "kubeconfig retrieved: $KUBECONFIG_PATH"

# --- 2. Automatically set the env variables ---
export ROSA_TA_ENABLE_AUTH=false
export ROSA_TA_KUBECONFIG="$KUBECONFIG_PATH"
export DATABASE_URL="$DB_PATH"

# --- 3. Start the server ---
log "Starting server"
SERVER_PID=""
cleanup() {
    if [ -n "$SERVER_PID" ]; then
        kill "$SERVER_PID" 2> /dev/null || true
        wait "$SERVER_PID" 2> /dev/null || true
    fi
}
trap cleanup EXIT

(cd "$REPO_ROOT" && go run ./cmd/server --log-level debug) > "$SERVER_LOG" 2>&1 &
SERVER_PID=$!

elapsed=0
until curl -sf "$SERVER_URL/health" > /dev/null 2>&1; do
    kill -0 "$SERVER_PID" 2> /dev/null || fail "server exited early — check $SERVER_LOG"
    if [ "$elapsed" -ge "$WAIT_TIMEOUT" ]; then
        fail "server did not become healthy within ${WAIT_TIMEOUT}s — check $SERVER_LOG"
    fi
    sleep 1
    elapsed=$((elapsed + 1))
done
ok "server is healthy"

# --- 4. Run a simple GET action against kind and check the API works ---
log "Running 'get' action (list pods in kube-system)"
response=$(curl -sf -X POST "$API_BASE/get/run" \
    -H 'Content-Type: application/json' \
    -d '{"target_cluster": "local", "params": {"version": "v1", "resource": "pods", "namespace": "kube-system"}}') \
    || fail "POST $API_BASE/get/run failed — check $SERVER_LOG"

execution_id=$(echo "$response" | jq -r '.id')
[ -n "$execution_id" ] && [ "$execution_id" != "null" ] || fail "no execution id in response: $response"

status="pending"
elapsed=0
while [ "$status" = "pending" ] || [ "$status" = "running" ]; do
    if [ "$elapsed" -ge "$WAIT_TIMEOUT" ]; then
        fail "execution $execution_id did not complete within ${WAIT_TIMEOUT}s (last status: $status)"
    fi
    sleep 1
    elapsed=$((elapsed + 1))
    response=$(curl -sf "$API_BASE/runs/$execution_id?include=output,logs") \
        || fail "GET $API_BASE/runs/$execution_id failed — check $SERVER_LOG"
    status=$(echo "$response" | jq -r '.status')
done

[ "$status" = "succeeded" ] || fail "execution $execution_id finished with status '$status': $response"
ok "get action succeeded (execution $execution_id)"

echo
ok "Integration smoke test passed."

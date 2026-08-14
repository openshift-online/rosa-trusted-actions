#!/bin/bash
set -e

# Creates (or reuses) a disposable kind cluster and starts the ministack compose stack,
# waiting for both to be ready before returning. See integration/README.md Phase 1.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLUSTER_NAME=${ROSA_TA_KIND_CLUSTER_NAME:-"rosa-ta"}
KUBECONFIG_PATH="$SCRIPT_DIR/.kind-kubeconfig"
COMPOSE_FILE="$SCRIPT_DIR/podman-compose.yml"
MINISTACK_CONTAINER="rosa-ta-ministack"
WAIT_TIMEOUT=${ROSA_TA_ITEST_WAIT_TIMEOUT:-120}

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() { echo -e "${YELLOW}==>${NC} $*"; }
ok() { echo -e "${GREEN}✓${NC} $*"; }
fail() { echo -e "${RED}✗ $*${NC}" >&2; exit 1; }

for bin in kind kubectl podman-compose; do
    command -v "$bin" > /dev/null 2>&1 || fail "'$bin' is required but not found on PATH"
done

# --- kind cluster ---
if kind get clusters 2> /dev/null | grep -qx "$CLUSTER_NAME"; then
    log "kind cluster '$CLUSTER_NAME' already exists, reusing it"
    kind get kubeconfig --name "$CLUSTER_NAME" > "$KUBECONFIG_PATH"
else
    log "Creating kind cluster '$CLUSTER_NAME'"
    kind create cluster --name "$CLUSTER_NAME" --kubeconfig "$KUBECONFIG_PATH" --wait "${WAIT_TIMEOUT}s"
fi

log "Waiting for kube-system pods to be ready"
kubectl --kubeconfig "$KUBECONFIG_PATH" wait --for=condition=Ready pods --all -n kube-system --timeout "${WAIT_TIMEOUT}s"
ok "kind cluster '$CLUSTER_NAME' is ready (kubeconfig: $KUBECONFIG_PATH)"

# --- ministack ---
log "Starting ministack via podman-compose"
podman-compose -f "$COMPOSE_FILE" up -d

log "Waiting for ministack to report healthy"
elapsed=0
while true; do
    status=$(podman inspect --format '{{.State.Health.Status}}' "$MINISTACK_CONTAINER" 2> /dev/null || echo "unknown")
    [ "$status" = "healthy" ] && break
    if [ "$elapsed" -ge "$WAIT_TIMEOUT" ]; then
        fail "ministack did not become healthy within ${WAIT_TIMEOUT}s (status: $status; check: podman logs $MINISTACK_CONTAINER)"
    fi
    sleep 2
    elapsed=$((elapsed + 2))
done
ok "ministack is healthy"

echo
ok "Integration environment ready."
echo "Run the server with:"
echo "  export ROSA_TA_ENABLE_AUTH=false"
echo "  export ROSA_TA_KUBECONFIG=$KUBECONFIG_PATH"
echo "  go run ./cmd/server --log-level debug"

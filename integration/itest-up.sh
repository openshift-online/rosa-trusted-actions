#!/bin/bash
set -e

# Creates (or reuses) a disposable kind cluster and starts the localstack compose stack,
# waiting for both to be ready before returning. See integration/README.md Phase 1.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLUSTER_NAME=${ROSA_TA_KIND_CLUSTER_NAME:-"rosa-ta"}
KIND_IMAGE="kindest/node:v1.33.1"
KUBECONFIG_PATH="$SCRIPT_DIR/.kind-kubeconfig"
COMPOSE_FILE="$SCRIPT_DIR/podman-compose.yml"
LOCALSTACK_CONTAINER="rosa-ta-localstack"
WAIT_TIMEOUT=${ROSA_TA_ITEST_WAIT_TIMEOUT:-120}

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() { echo -e "${YELLOW}==>${NC} $*"; }
ok() { echo -e "${GREEN}✓${NC} $*"; }
fail() { echo -e "${RED}✗ $*${NC}" >&2; exit 1; }

for bin in kind kubectl podman podman-compose; do
    command -v "$bin" > /dev/null 2>&1 || fail "'$bin' is required but not found on PATH"
done

# --- kind cluster ---
old_umask=$(umask)
umask 077
if kind get clusters 2> /dev/null | grep -qx "$CLUSTER_NAME"; then
    log "kind cluster '$CLUSTER_NAME' already exists, reusing it"
    kind get kubeconfig --name "$CLUSTER_NAME" > "$KUBECONFIG_PATH"
else
    log "Creating kind cluster '$CLUSTER_NAME'"
    kind create cluster --name "$CLUSTER_NAME" --kubeconfig "$KUBECONFIG_PATH" --wait "${WAIT_TIMEOUT}s" --image "$KIND_IMAGE"
fi
umask "$old_umask"
chmod 0600 "$KUBECONFIG_PATH"

log "Waiting for kube-system pods to be ready"
kubectl --kubeconfig "$KUBECONFIG_PATH" wait --for=condition=Ready pods --all -n kube-system --timeout "${WAIT_TIMEOUT}s"
ok "kind cluster '$CLUSTER_NAME' is ready (kubeconfig: $KUBECONFIG_PATH)"

# --- localstack ---
log "Starting localstack via podman-compose"
podman-compose -f "$COMPOSE_FILE" up -d

log "Waiting for localstack to report healthy"
elapsed=0
while true; do
    status=$(podman inspect --format '{{.State.Health.Status}}' "$LOCALSTACK_CONTAINER" 2> /dev/null || echo "unknown")
    [ "$status" = "healthy" ] && break
    if [ "$elapsed" -ge "$WAIT_TIMEOUT" ]; then
        fail "localstack did not become healthy within ${WAIT_TIMEOUT}s (status: $status; check: podman logs $LOCALSTACK_CONTAINER)"
    fi
    sleep 2
    elapsed=$((elapsed + 2))
done
ok "localstack is healthy"

echo
ok "Integration environment ready."
echo "Start the server with: make itest-run"

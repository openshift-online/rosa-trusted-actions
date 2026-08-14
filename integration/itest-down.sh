#!/bin/bash
set -e

# Tears down the kind cluster and ministack compose stack created by itest-up.sh, and removes
# generated local artifacts. Safe to run even if nothing is up. See integration/README.md Phase 1.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLUSTER_NAME=${ROSA_TA_KIND_CLUSTER_NAME:-"rosa-ta"}
KUBECONFIG_PATH="$SCRIPT_DIR/.kind-kubeconfig"
COMPOSE_FILE="$SCRIPT_DIR/podman-compose.yml"
MINISTACK_CONTAINER="rosa-ta-ministack"
DB_PATH="$SCRIPT_DIR/.trusted_actions.db"
SERVER_LOG="$SCRIPT_DIR/.server.log"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() { echo -e "${YELLOW}==>${NC} $*"; }
ok() { echo -e "${GREEN}✓${NC} $*"; }
fail() { echo -e "${RED}✗ $*${NC}" >&2; exit 1; }

for bin in kind podman podman-compose; do
    command -v "$bin" > /dev/null 2>&1 || fail "'$bin' is required but not found on PATH"
done

# --- ministack ---
if podman container exists "$MINISTACK_CONTAINER" 2> /dev/null; then
    log "Stopping ministack"
    podman-compose -f "$COMPOSE_FILE" down
    ok "ministack stopped"
else
    log "ministack container not found, nothing to stop"
fi

# --- kind cluster ---
if kind get clusters 2> /dev/null | grep -qx "$CLUSTER_NAME"; then
    log "Deleting kind cluster '$CLUSTER_NAME'"
    kind delete cluster --name "$CLUSTER_NAME"
    ok "kind cluster '$CLUSTER_NAME' deleted"
else
    log "kind cluster '$CLUSTER_NAME' not found, nothing to delete"
fi

# --- generated artifacts ---
rm -f "$KUBECONFIG_PATH" "$DB_PATH" "$DB_PATH-shm" "$DB_PATH-wal" "$SERVER_LOG"
ok "removed generated local artifacts"

echo
ok "Integration environment torn down."

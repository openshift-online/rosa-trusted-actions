#!/bin/bash
# Fetch staging credentials from Vault and export the env vars needed to run
# the server locally against the staging backplane.
#
# Usage: source scripts/setup-local-staging.sh
#
# Prerequisites: vault CLI, jq, OIDC access to vault.devshift.net

_rta_setup_staging() {
    local ocm_vault_path="osd-sre/trusted-actions/ocm/staging"

    export VAULT_ADDR="https://vault.devshift.net"

    local token
    token=$(vault login -method=oidc -token-only) || { unset VAULT_ADDR; echo "ERROR: vault login failed"; return 1; }
    export VAULT_TOKEN="$token"

    # OCM service account credentials (AMS role checks)
    local vault_raw
    vault_raw=$(vault kv get -format=json "$ocm_vault_path") || { unset VAULT_ADDR VAULT_TOKEN; echo "ERROR: vault kv get failed"; return 1; }

    local ocm_secrets
    ocm_secrets=$(echo "$vault_raw" | jq -r '.data.data') || { unset VAULT_ADDR VAULT_TOKEN; echo "ERROR: failed to parse vault response"; return 1; }

    local client_id client_secret
    client_id=$(echo "$ocm_secrets" | jq -e -r '.CLIENT_ID // empty') || { unset VAULT_ADDR VAULT_TOKEN; echo "ERROR: CLIENT_ID missing from vault"; return 1; }
    client_secret=$(echo "$ocm_secrets" | jq -e -r '.CLIENT_SECRET // empty') || { unset VAULT_ADDR VAULT_TOKEN; echo "ERROR: CLIENT_SECRET missing from vault"; return 1; }
    export ROSA_TA_OCM_CLIENT_ID="$client_id"
    export ROSA_TA_OCM_CLIENT_SECRET="$client_secret"
    export ROSA_TA_OCM_BASE_URL="https://api.stage.openshift.com"

    # TODO: decide if these are different than the ones we defined above ... pending adding roles to the RTA SA
    #   export ROSA_TA_BACKPLANE_URL=...
    #   export ROSA_TA_BACKPLANE_CLIENT_ID=...
    #   export ROSA_TA_BACKPLANE_CLIENT_SECRET=...

    unset VAULT_ADDR VAULT_TOKEN

    export ROSA_TA_ALLOWED_NAMESPACES="openshift-logging,openshift-monitoring,openshift-operators,openshift-config"
    export ROSA_TA_ALLOWED_SECRETS="openshift-config/pull-secret"

    echo "Staging OCM credentials loaded."
    if [ -z "$ROSA_TA_BACKPLANE_URL" ]; then
        echo "WARNING: Backplane credentials not set. Set ROSA_TA_BACKPLANE_URL, ROSA_TA_BACKPLANE_CLIENT_ID, ROSA_TA_BACKPLANE_CLIENT_SECRET manually."
    fi
    echo "Start server: make run"
}

_rta_setup_staging
unset -f _rta_setup_staging

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
    export VAULT_TOKEN="$(vault login -method=oidc -token-only)" || return 1

    # OCM service account credentials (AMS role checks)
    local ocm_secrets
    ocm_secrets=$(vault kv get -format=json "$ocm_vault_path" | jq -r '.data.data') || return 1

    export ROSA_TA_OCM_CLIENT_ID=$(echo "$ocm_secrets" | jq -r '.CLIENT_ID')
    export ROSA_TA_OCM_CLIENT_SECRET=$(echo "$ocm_secrets" | jq -r '.CLIENT_SECRET')
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

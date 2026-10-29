#!/bin/bash
set -e

# Retrieves a kubeconfig from the kind cluster created by itest-up.sh, starts the server
# against it with mock auth, and runs smoke tests for all workflow actions across both
# execution scopes — the Kubernetes actions against kind and 'aws-get' against
# localstack — to confirm the API works end-to-end. See integration/README.md Phase 1.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CLUSTER_NAME=${ROSA_TA_KIND_CLUSTER_NAME:-"rosa-ta"}
KUBECONFIG_PATH="$SCRIPT_DIR/.kind-kubeconfig"
DB_PATH="$SCRIPT_DIR/.trusted_actions.db"
SERVER_LOG="$SCRIPT_DIR/.server.log"
SERVER_BIN="$SCRIPT_DIR/.server-bin"
SERVER_URL="http://localhost:8080"
API_BASE="$SERVER_URL/api/v0/trusted-actions"
WAIT_TIMEOUT=${ROSA_TA_ITEST_WAIT_TIMEOUT:-60}
LOCALSTACK_CONTAINER="rosa-ta-localstack"
# 127.0.0.1 rather than localhost: localstack binds IPv4 only, and on hosts where
# localhost resolves to ::1 first the connection is refused.
LOCALSTACK_URL="http://127.0.0.1:4566"
AWS_TEST_REGION="us-east-1"
AWS_TEST_TAG="rosa-ta-itest"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() { echo -e "${YELLOW}==>${NC} $*"; }
ok() { echo -e "${GREEN}✓${NC} $*"; }
fail() { echo -e "${RED}✗ $*${NC}" >&2; exit 1; }

for bin in kind go curl jq podman; do
    command -v "$bin" > /dev/null 2>&1 || fail "'$bin' is required but not found on PATH"
done

# --- Helper: run an action and wait for completion ---
# Usage: run_action <action-name> <json-body>
# Sets: EXEC_ID, EXEC_RESPONSE
run_action() {
    local action="$1"
    local body="$2"

    local response
    response=$(curl -sf -X POST "$API_BASE/$action/run" \
        -H 'Content-Type: application/json' \
        -d "$body") \
        || fail "POST $API_BASE/$action/run failed — check $SERVER_LOG"

    EXEC_ID=$(echo "$response" | jq -r '.id')
    [ -n "$EXEC_ID" ] && [ "$EXEC_ID" != "null" ] || fail "no execution id in response: $response"

    local status="pending"
    local elapsed=0
    while [ "$status" = "pending" ] || [ "$status" = "running" ]; do
        if [ "$elapsed" -ge "$WAIT_TIMEOUT" ]; then
            fail "execution $EXEC_ID ($action) did not complete within ${WAIT_TIMEOUT}s (last status: $status)"
        fi
        sleep 1
        elapsed=$((elapsed + 1))
        EXEC_RESPONSE=$(curl -sf "$API_BASE/runs/$EXEC_ID") \
            || fail "GET $API_BASE/runs/$EXEC_ID failed — check $SERVER_LOG"
        status=$(echo "$EXEC_RESPONSE" | jq -r '.status')
    done

    [ "$status" = "succeeded" ] || fail "execution $EXEC_ID ($action) finished with status '$status': $EXEC_RESPONSE"
}

# --- Helper: run an action and return its final status ---
# Usage: read -r status execution_id <<< "$(run_action_status <action> <json-body>)"
#
# run_action above treats anything but success as a script failure, which is
# what every positive test wants. The AWS section also asserts that a bad
# request *fails*, so that case needs a path that reports the status instead of
# dying on it.
run_action_status() {
    local action=$1 body=$2 response execution_id status elapsed=0

    response=$(curl -sf -X POST "$API_BASE/$action/run" \
        -H 'Content-Type: application/json' -d "$body") \
        || fail "POST $API_BASE/$action/run failed — check $SERVER_LOG"

    execution_id=$(echo "$response" | jq -r '.id')
    [ -n "$execution_id" ] && [ "$execution_id" != "null" ] \
        || fail "no execution id in response: $response"

    status="pending"
    while [ "$status" = "pending" ] || [ "$status" = "running" ]; do
        if [ "$elapsed" -ge "$WAIT_TIMEOUT" ]; then
            fail "execution $execution_id did not complete within ${WAIT_TIMEOUT}s (last status: $status)"
        fi
        sleep 1
        elapsed=$((elapsed + 1))
        response=$(curl -sf "$API_BASE/runs/$execution_id") \
            || fail "GET $API_BASE/runs/$execution_id failed — check $SERVER_LOG"
        status=$(echo "$response" | jq -r '.status')
    done

    echo "$status $execution_id"
}

# --- Helper: get execution output ---
# Usage: get_output <exec-id>
# Sets: EXEC_OUTPUT
get_output() {
    EXEC_OUTPUT=$(curl -sf "$API_BASE/runs/$1/output") \
        || fail "GET $API_BASE/runs/$1/output failed — check $SERVER_LOG"
}

# --- 1. Retrieve a kubeconfig from kind ---
kind get clusters 2> /dev/null | grep -qx "$CLUSTER_NAME" \
    || fail "kind cluster '$CLUSTER_NAME' not found — run 'make itest-up' first"
old_umask=$(umask)
umask 077
kind get kubeconfig --name "$CLUSTER_NAME" > "$KUBECONFIG_PATH"
umask "$old_umask"
chmod 0600 "$KUBECONFIG_PATH"
ok "kubeconfig retrieved: $KUBECONFIG_PATH"

# --- 2. Automatically set the env variables ---
export ROSA_TA_AUTH=disabled
export ROSA_TA_KUBECONFIG="$KUBECONFIG_PATH"
export DATABASE_URL="$DB_PATH"

# Mock auth grants every unauthenticated caller the SREP role, so the server refuses
# to start unless AWS traffic is pinned to the local emulator. The credentials are
# placeholders localstack accepts and ignores — without them the SDK would fall back
# to its default chain and hang on instance metadata.
export ROSA_TA_AWS_ENDPOINT_URL="$LOCALSTACK_URL"
export AWS_REGION="$AWS_TEST_REGION"
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test

# The region allowlist has no built-in default, so the server refuses to start
# without one. Set to the same region the fixtures are seeded in.
export ROSA_TA_AWS_ALLOWED_REGIONS="$AWS_TEST_REGION"

# --- 3. Start the server ---
log "Starting server"
SERVER_PID=""
cleanup() {
    if [ -n "$SERVER_PID" ]; then
        kill "$SERVER_PID" 2> /dev/null || true
        wait "$SERVER_PID" 2> /dev/null || true
    fi
    rm -f "$SERVER_BIN"
}
trap cleanup EXIT

go build -C "$REPO_ROOT" -o "$SERVER_BIN" ./cmd/server || fail "failed to build server binary"

"$SERVER_BIN" --log-level debug --listen-addr ":8080" > "$SERVER_LOG" 2>&1 &
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

# --- 4. Test: get action (list pods in kube-system) ---
log "Running 'get' action (list pods in kube-system)"
run_action "get" '{"target_cluster": "local", "params": {"version": "v1", "resource": "pods", "namespace": "kube-system"}}'
ok "get action succeeded (execution $EXEC_ID)"

# --- 5. Test: describe-nodes action ---
log "Running 'describe-nodes' action (all nodes)"
run_action "describe-nodes" '{"target_cluster": "local", "jira": "ITEST-1"}'
get_output "$EXEC_ID"

node_count=$(echo "$EXEC_OUTPUT" | jq '.resources | length')
[ "$node_count" -ge 1 ] || fail "describe-nodes returned $node_count nodes, expected at least 1"

first_node_name=$(echo "$EXEC_OUTPUT" | jq -r '.resources[0].name')
[ -n "$first_node_name" ] && [ "$first_node_name" != "null" ] \
    || fail "describe-nodes: first node has no name"

has_conditions=$(echo "$EXEC_OUTPUT" | jq '.resources[0] | has("conditions")')
[ "$has_conditions" = "true" ] || fail "describe-nodes: first node has no conditions"

has_capacity=$(echo "$EXEC_OUTPUT" | jq '.resources[0] | has("capacity")')
[ "$has_capacity" = "true" ] || fail "describe-nodes: first node has no capacity"

has_pods=$(echo "$EXEC_OUTPUT" | jq '.resources[0] | has("pods")')
[ "$has_pods" = "true" ] || fail "describe-nodes: first node has no pods array"

ok "describe-nodes action succeeded: $node_count node(s), first node '$first_node_name' has conditions, capacity, and pods (execution $EXEC_ID)"

# Test: describe-nodes with a specific node name
log "Running 'describe-nodes' action (single node: $first_node_name)"
run_action "describe-nodes" "{\"target_cluster\": \"local\", \"jira\": \"ITEST-2\", \"params\": {\"name\": \"$first_node_name\"}}"
get_output "$EXEC_ID"

single_node_count=$(echo "$EXEC_OUTPUT" | jq '.resources | length')
[ "$single_node_count" -eq 1 ] || fail "describe-nodes (single): expected 1 node, got $single_node_count"

returned_name=$(echo "$EXEC_OUTPUT" | jq -r '.resources[0].name')
[ "$returned_name" = "$first_node_name" ] \
    || fail "describe-nodes (single): expected node '$first_node_name', got '$returned_name'"

ok "describe-nodes (single node) succeeded (execution $EXEC_ID)"

# --- 6. Test: get-pull-secret-email action ---
log "Running 'get-pull-secret-email' action"
run_action "get-pull-secret-email" '{"target_cluster": "local", "jira": "ITEST-3"}'
get_output "$EXEC_ID"

email=$(echo "$EXEC_OUTPUT" | jq -r '.resources[0].email')
[ "$email" = "itest@example.com" ] \
    || fail "get-pull-secret-email: expected 'itest@example.com', got '$email'"

ok "get-pull-secret-email action succeeded: email=$email (execution $EXEC_ID)"

# --- 7. Test: list-alerts action ---
log "Running 'list-alerts' action (firing alerts)"
run_action "list-alerts" '{"target_cluster": "local", "jira": "ITEST-4", "params": {"namespace": "openshift-monitoring", "state": "firing"}}'
get_output "$EXEC_ID"

critical_count=$(echo "$EXEC_OUTPUT" | jq '.resources[0].alerts.critical | length')
warning_count=$(echo "$EXEC_OUTPUT" | jq '.resources[0].alerts.warning | length')

[ "$critical_count" -ge 1 ] || fail "list-alerts: expected at least 1 critical alert, got $critical_count"
[ "$warning_count" -ge 1 ] || fail "list-alerts: expected at least 1 warning alert, got $warning_count"

ok "list-alerts action succeeded: $critical_count critical, $warning_count warning alerts (execution $EXEC_ID)"

# Test: list-alerts with severity filter
log "Running 'list-alerts' action (critical only)"
run_action "list-alerts" '{"target_cluster": "local", "jira": "ITEST-5", "params": {"namespace": "openshift-monitoring", "state": "firing", "severity": "critical"}}'
get_output "$EXEC_ID"

critical_only=$(echo "$EXEC_OUTPUT" | jq '.resources[0].alerts.critical | length')
warning_filtered=$(echo "$EXEC_OUTPUT" | jq '.resources[0].alerts.warning | length')

[ "$critical_only" -ge 1 ] || fail "list-alerts (critical): expected at least 1 critical alert, got $critical_only"
[ "$warning_filtered" -eq 0 ] || fail "list-alerts (critical): expected 0 warning alerts, got $warning_filtered"

ok "list-alerts (severity=critical) succeeded: $critical_only critical, $warning_filtered warning (execution $EXEC_ID)"

# --- 8. Run an AWS GET action against localstack ---
# Seeded here rather than in itest-up.sh so the fixture sits next to the assertion
# that depends on it. Re-running the script simply adds another instance, which the
# tag filter ignores.
log "Seeding an EC2 instance in localstack"
podman inspect "$LOCALSTACK_CONTAINER" > /dev/null 2>&1 \
    || fail "localstack container '$LOCALSTACK_CONTAINER' not found — run 'make itest-up' first"

# localstack ships its own AMIs; ask for one rather than hardcoding an id that may
# change between image versions.
ami_id=$(podman exec "$LOCALSTACK_CONTAINER" awslocal ec2 describe-images \
    --query 'Images[0].ImageId' --output text 2> /dev/null) \
    || fail "failed to list AMIs in localstack"
[ -n "$ami_id" ] && [ "$ami_id" != "None" ] || fail "no AMI available in localstack"

instance_id=$(podman exec "$LOCALSTACK_CONTAINER" awslocal ec2 run-instances \
    --image-id "$ami_id" --count 1 --instance-type m5.large \
    --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=$AWS_TEST_TAG}]" \
    --query 'Instances[0].InstanceId' --output text 2> /dev/null) \
    || fail "failed to seed an EC2 instance in localstack"
ok "seeded EC2 instance $instance_id"

log "Running 'aws-get' action (list ec2-instances)"
run_action "aws-get" \
    "{\"target_cluster\": \"local\", \"params\": {\"resource\": \"ec2-instances\", \"filters\": \"tag:Name=$AWS_TEST_TAG\"}}"
get_output "$EXEC_ID"

# Assert on the output, not just the status: a read action that succeeds while
# returning nothing would otherwise pass.
echo "$EXEC_OUTPUT" | jq -e --arg id "$instance_id" \
    'any(.resources[]; .InstanceId == $id)' > /dev/null \
    || fail "instance $instance_id not found in aws-get output: $EXEC_OUTPUT"
ok "aws-get action succeeded and returned $instance_id (execution $EXEC_ID)"

# --- 9. Cover the rest of the aws-get resources localstack can serve ---
# The community image has no elb, elbv2 or cloudtrail, so load-balancers,
# load-balancers-classic, target-groups, target-health and cloudtrail-events
# cannot be exercised here — they are covered by unit tests against fakes.
# See integration/README.md.

# assert_aws_get <resource> <extra-params-json> <jq-filter> — runs aws-get and
# checks the output, not just the status: a read that succeeds while returning
# nothing would otherwise pass.
assert_aws_get() {
    local resource=$1 extra=$2 filter=$3

    run_action "aws-get" \
        "{\"target_cluster\": \"local\", \"params\": {\"resource\": \"$resource\"$extra}}"
    get_output "$EXEC_ID"

    echo "$EXEC_OUTPUT" | jq -e "$filter" > /dev/null \
        || fail "aws-get $resource returned unexpected output: $EXEC_OUTPUT"
    ok "aws-get $resource succeeded (execution $EXEC_ID)"
}

# A default localstack already has a VPC, subnets, a route table and a security
# group, so these need no fixture.
log "Running 'aws-get' for the remaining EC2 resources"
for resource in ec2-vpcs ec2-subnets ec2-route-tables ec2-security-groups; do
    assert_aws_get "$resource" "" '.resources | length > 0'
done

# DHCP option sets and hosted zones are not created by default, so both are
# seeded here. Re-running the script adds another of each, which is harmless:
# the assertions only require at least one.
log "Seeding a DHCP options set in localstack"
dhcp_id=$(podman exec "$LOCALSTACK_CONTAINER" awslocal ec2 create-dhcp-options \
    --dhcp-configurations 'Key=domain-name-servers,Values=[10.0.0.2]' \
    --tag-specifications "ResourceType=dhcp-options,Tags=[{Key=Name,Value=$AWS_TEST_TAG}]" \
    --query 'DhcpOptions.DhcpOptionsId' --output text 2> /dev/null) \
    || fail "failed to seed a DHCP options set in localstack"
assert_aws_get ec2-dhcp-options "" \
    "any(.resources[]; .DhcpOptionsId == \"$dhcp_id\")"

log "Seeding a Route 53 hosted zone in localstack"
zone_id=$(podman exec "$LOCALSTACK_CONTAINER" awslocal route53 create-hosted-zone \
    --name "$AWS_TEST_TAG.example.com" --caller-reference "$AWS_TEST_TAG-$$" \
    --query 'HostedZone.Id' --output text 2> /dev/null) \
    || fail "failed to seed a hosted zone in localstack"
assert_aws_get route53-hosted-zones "" \
    "any(.resources[]; .Id == \"$zone_id\")"

# route53-record-sets is the one read with a required id and a hand-written
# pagination loop, so it is asserted specifically rather than in the loop above.
# A new zone always has its NS and SOA records.
assert_aws_get route53-record-sets ", \"id\": \"$zone_id\"" \
    '[.resources[].Type] | contains(["NS", "SOA"])'

# --- 10. A rejected request must fail the execution, not quietly return more ---
# aws-get refuses a param the resource does not accept. That guarantee is only
# worth having if it survives the whole HTTP path, so it is checked here too.
# run_action_status rather than run_action, because this one must not succeed.
log "Running 'aws-get' with a param the resource does not accept"
read -r status execution_id <<< "$(run_action_status aws-get \
    '{"target_cluster": "local", "params": {"resource": "route53-hosted-zones", "filters": "tag:Name=x"}}')"

[ "$status" = "failed" ] \
    || fail "aws-get with an unaccepted param finished as '$status', expected 'failed'"
ok "aws-get rejected an unaccepted param (execution $execution_id)"

echo
ok "All integration tests passed."

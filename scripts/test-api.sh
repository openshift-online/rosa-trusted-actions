#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Server configuration
SERVER_URL=${SERVER_URL:-"http://localhost:8080"}
API_BASE="$SERVER_URL/api/v0/trusted-actions"

# Example of setting environment variables
export AWS_REGION=${AWS_REGION:-"us-east-1"}
export ROSA_TA_S3_BUCKET=${ROSA_TA_S3_BUCKET:-"test-bucket"}

echo -e "${YELLOW}Testing ROSA Trusted Actions Server${NC}"
echo "Server: $SERVER_URL"
echo "======================================="

# Function to test an endpoint
test_endpoint() {
    local name="$1"
    local method="$2"
    local url="$3"
    local data="$4"

    echo -n "Testing $name... "

    if [ "$method" = "POST" ]; then
        response=$(curl -s -w "%{http_code}" -X POST "$url" \
            -H "Content-Type: application/json" \
            -d "$data" \
            -o /tmp/api_response.json)
    else
        response=$(curl -s -w "%{http_code}" "$url" -o /tmp/api_response.json)
    fi

    http_code="${response: -3}"

    if [ "$http_code" -ge 200 ] && [ "$http_code" -lt 300 ]; then
        echo -e "${GREEN}✓ ($http_code)${NC}"
        # Pretty print JSON if jq is available
        if command -v jq &> /dev/null; then
            echo "Response:" && jq . /tmp/api_response.json | head -20
        else
            echo "Response:" && cat /tmp/api_response.json | head -20
        fi
        echo ""
        return 0
    else
        echo -e "${RED}✗ ($http_code)${NC}"
        echo "Response:" && cat /tmp/api_response.json
        echo ""
        return 1
    fi
}

# Check if server is running
echo -n "Checking server health... "
if curl -s "$SERVER_URL/health" > /dev/null; then
    echo -e "${GREEN}✓ Server is running${NC}"
else
    echo -e "${RED}✗ Server is not responding${NC}"
    echo "Make sure the server is running with: make run"
    exit 1
fi

echo ""

# Test all endpoints
test_endpoint "Health Check" "GET" "$SERVER_URL/health"
test_endpoint "List Actions" "GET" "$API_BASE/"
test_endpoint "Describe Action" "GET" "$API_BASE/cluster-info"
test_endpoint "Execute Action" "POST" "$API_BASE/cluster-info/run" '{
    "target_cluster": "test-cluster",
    "params": {"namespace": "default"},
    "dry_run": true
}'
test_endpoint "List Executions" "GET" "$API_BASE/runs?limit=5"
test_endpoint "Get Execution" "GET" "$API_BASE/runs/$(uuidgen)?include=output,logs"
test_endpoint "Audit Log" "GET" "$API_BASE/audit?limit=5"

echo -e "${GREEN}All tests completed!${NC}"

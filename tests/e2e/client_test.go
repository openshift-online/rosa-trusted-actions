//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

const apiBase = "/api/v0/trusted-actions"

// execution is the subset of the Execution schema the suite asserts on.
// Decoded into a local type rather than the generated one so the tests fail
// when the wire format changes, instead of silently following it.
type execution struct {
	ID            string `json:"id"`
	Action        string `json:"action"`
	TargetCluster string `json:"target_cluster"`
	Status        string `json:"status"`
}

type executionOutput struct {
	Message   string                   `json:"message"`
	Resources []map[string]interface{} `json:"resources"`
}

// Terminal execution states, per the ExecutionStatus schema.
func isTerminal(status string) bool {
	return status == "succeeded" || status == "failed" || status == "timed_out"
}

// runAction submits an action, waits for it to finish, requires success, and
// returns its output. This is the happy path; a test that expects a refusal
// uses submit and waitForTerminal directly.
func runAction(t *testing.T, action string, params map[string]string) executionOutput {
	t.Helper()

	id := submit(t, action, params)
	final := waitForTerminal(t, id)
	if final.Status != "succeeded" {
		// The failure reason is not exposed by the API — the worker logs it
		// and the executions table has no column for it (see the TODO in
		// internal/worker.Pool.process). Point at the server log, which CI
		// archives, so a red test is diagnosable at all.
		t.Fatalf("action %q finished as %q, want succeeded (execution %s; reason is in the server log)",
			action, final.Status, id)
	}
	return fetchOutput(t, id)
}

// submit posts an execution request and returns the execution ID.
func submit(t *testing.T, action string, params map[string]string) string {
	t.Helper()

	body := map[string]interface{}{
		"target_cluster": env.clusterID,
		"jira":           env.jira,
	}
	if len(params) > 0 {
		body["params"] = params
	}

	status, payload := request(t, http.MethodPost, fmt.Sprintf("%s/%s/run", apiBase, action), body)
	if status != http.StatusAccepted {
		t.Fatalf("POST %s/run returned %d, want 202: %s", action, status, payload)
	}

	var created execution
	if err := json.Unmarshal(payload, &created); err != nil {
		t.Fatalf("decoding the execution for %q: %v (body: %s)", action, err, payload)
	}
	if created.ID == "" {
		t.Fatalf("no execution id in the response for %q: %s", action, payload)
	}
	return created.ID
}

// waitForTerminal polls an execution until it reaches a terminal state.
func waitForTerminal(t *testing.T, id string) execution {
	t.Helper()

	deadline := time.Now().Add(env.actionTimeout)
	for {
		status, payload := request(t, http.MethodGet, fmt.Sprintf("%s/runs/%s", apiBase, id), nil)
		if status != http.StatusOK {
			t.Fatalf("GET runs/%s returned %d, want 200: %s", id, status, payload)
		}

		var current execution
		if err := json.Unmarshal(payload, &current); err != nil {
			t.Fatalf("decoding execution %s: %v (body: %s)", id, err, payload)
		}
		if isTerminal(current.Status) {
			return current
		}
		if time.Now().After(deadline) {
			t.Fatalf("execution %s still %q after %s", id, current.Status, env.actionTimeout)
		}
		time.Sleep(env.pollInterval)
	}
}

// fetchOutput reads a terminal execution's output.
func fetchOutput(t *testing.T, id string) executionOutput {
	t.Helper()

	status, payload := request(t, http.MethodGet, fmt.Sprintf("%s/runs/%s/output", apiBase, id), nil)
	if status != http.StatusOK {
		t.Fatalf("GET runs/%s/output returned %d, want 200: %s", id, status, payload)
	}

	var output executionOutput
	if err := json.Unmarshal(payload, &output); err != nil {
		t.Fatalf("decoding the output of %s: %v (body: %s)", id, err, payload)
	}
	return output
}

// request performs an authenticated call and returns the status code and the
// raw body. The body comes back undecoded because the error responses are a
// different shape from the success ones, and a test asserting a refusal wants
// to read what the server actually said.
func request(t *testing.T, method, path string, body interface{}) (int, []byte) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encoding the request body for %s %s: %v", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, env.baseURL+path, reader)
	if err != nil {
		t.Fatalf("building the request for %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+env.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the response to %s %s: %v", method, path, err)
	}
	return resp.StatusCode, payload
}

// resourceName reads metadata.name from a resource an action returned. Kube
// actions return whole objects; the AWS scope returns SDK structs, so this is
// only meaningful for the Kubernetes actions.
func resourceName(resource map[string]interface{}) string {
	metadata, ok := resource["metadata"].(map[string]interface{})
	if !ok {
		return ""
	}
	name, _ := metadata["name"].(string)
	return name
}

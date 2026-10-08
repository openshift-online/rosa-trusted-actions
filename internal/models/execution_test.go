package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

func TestExecution_ToOpenAPI_AllFieldsSet(t *testing.T) {
	now := time.Now().UTC()
	completedAt := now.Add(time.Minute)
	raw := json.RawMessage(`{"k":"v"}`)

	e := &Execution{
		ID:               uuid.New(),
		Action:           "get_pods",
		Status:           string(openapi.ExecutionStatusPending),
		ApprovalState:    new(string(openapi.ApprovalStateNotRequired)),
		Username:         new("alice"),
		TargetCluster:    "cluster-1",
		Jira:             new("JIRA-1"),
		DryRun:           new(true),
		Force:            new(false),
		Params:           &raw,
		Scope:            new("kubernetes_api"),
		Type:             new("read_only"),
		Revision:         new("a1b2c3d"),
		ManifestWorkName: new("ta-abc123"),
		RunnerSeconds:    new(5),
		UploadSeconds:    new(12),
		DurationSeconds:  new(29),
		CreatedAt:        now,
		UpdatedAt:        now,
		CompletedAt:      &completedAt,
	}

	out := e.ToOpenAPI()

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Id", out.Id, e.ID},
		{"Action", out.Action, e.Action},
		{"Status", out.Status, openapi.ExecutionStatus(e.Status)},
		{"TargetCluster", out.TargetCluster, e.TargetCluster},
		{"ApprovalState", string(*out.ApprovalState), *e.ApprovalState},
		{"Username", *out.Username, *e.Username},
		{"Jira", *out.Jira, *e.Jira},
		{"DryRun", *out.DryRun, *e.DryRun},
		{"Force", *out.Force, *e.Force},
		{"Scope", string(*out.Scope), *e.Scope},
		{"Type", string(*out.Type), *e.Type},
		{"Revision", *out.Revision, *e.Revision},
		{"ManifestWorkName", *out.ManifestWorkName, *e.ManifestWorkName},
		{"RunnerSeconds", *out.RunnerSeconds, *e.RunnerSeconds},
		{"UploadSeconds", *out.UploadSeconds, *e.UploadSeconds},
		{"DurationSeconds", *out.DurationSeconds, *e.DurationSeconds},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	if out.Params == nil {
		t.Fatalf("Params = nil, want non-nil")
	}
	if (*out.Params)["k"] != "v" {
		t.Errorf("Params[k] = %v, want v", (*out.Params)["k"])
	}
	if !out.CreatedAt.Equal(e.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", out.CreatedAt, e.CreatedAt)
	}
	if !out.UpdatedAt.Equal(e.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", out.UpdatedAt, e.UpdatedAt)
	}
	if out.CompletedAt == nil || !out.CompletedAt.Equal(*e.CompletedAt) {
		t.Errorf("CompletedAt = %v, want %v", out.CompletedAt, e.CompletedAt)
	}
}

func TestExecution_ToOpenAPI_NilOptionals(t *testing.T) {
	e := &Execution{
		ID:            uuid.New(),
		Action:        "get_pods",
		Status:        string(openapi.ExecutionStatusPending),
		TargetCluster: "cluster-1",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	out := e.ToOpenAPI()

	if out.ApprovalState != nil {
		t.Errorf("ApprovalState = %v, want nil", out.ApprovalState)
	}
	if out.Scope != nil {
		t.Errorf("Scope = %v, want nil", out.Scope)
	}
	if out.Type != nil {
		t.Errorf("Type = %v, want nil", out.Type)
	}
	if out.Params != nil {
		t.Errorf("Params = %v, want nil", out.Params)
	}
}

func TestExecution_ToOpenAPI_ParamsInvalidJSON(t *testing.T) {
	raw := json.RawMessage("{invalid")
	e := &Execution{
		ID:            uuid.New(),
		Action:        "get_pods",
		Status:        string(openapi.ExecutionStatusPending),
		TargetCluster: "cluster-1",
		Params:        &raw,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	out := e.ToOpenAPI()

	if out.Params != nil {
		t.Errorf("Params = %v, want nil", out.Params)
	}
}

func TestExecutionFromRequest_Defaults(t *testing.T) {
	req := openapi.ExecutionRequest{TargetCluster: "c1", Jira: "JIRA-1"}

	exec := ExecutionFromRequest("get", req, "alice")

	if exec.Status != string(openapi.ExecutionStatusPending) {
		t.Errorf("Status = %v, want %v", exec.Status, openapi.ExecutionStatusPending)
	}
	if exec.ApprovalState == nil || *exec.ApprovalState != string(openapi.ApprovalStateNotRequired) {
		t.Errorf("ApprovalState = %v, want %v", exec.ApprovalState, openapi.ApprovalStateNotRequired)
	}
	if exec.Username == nil || *exec.Username != "alice" {
		t.Errorf("Username = %v, want alice", exec.Username)
	}
	if exec.Action != "get" {
		t.Errorf("Action = %v, want get", exec.Action)
	}
	if exec.TargetCluster != "c1" {
		t.Errorf("TargetCluster = %v, want c1", exec.TargetCluster)
	}
	if exec.Jira == nil || *exec.Jira != "JIRA-1" {
		t.Errorf("Jira = %v, want JIRA-1", exec.Jira)
	}
	if exec.Params != nil {
		t.Errorf("Params = %v, want nil", exec.Params)
	}
	if exec.ID == uuid.Nil {
		t.Errorf("ID = %v, want non-nil uuid", exec.ID)
	}
}

func TestExecutionFromRequest_WithParams(t *testing.T) {
	params := map[string]string{"k": "v"}
	req := openapi.ExecutionRequest{
		TargetCluster: "c1",
		Jira:          "JIRA-1",
		Params:        &params,
	}

	exec := ExecutionFromRequest("get", req, "alice")

	if exec.Params == nil {
		t.Fatalf("Params = nil, want non-nil")
	}

	out := exec.ToOpenAPI()
	if out.Params == nil {
		t.Fatalf("out.Params = nil, want non-nil")
	}
	if (*out.Params)["k"] != "v" {
		t.Errorf("out.Params[k] = %v, want v", (*out.Params)["k"])
	}
}

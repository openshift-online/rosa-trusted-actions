package models

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

func TestAuditEntry_ToOpenAPI_AllFields(t *testing.T) {
	a := &AuditEntry{
		ID:            uuid.New(),
		Timestamp:     time.Now().UTC(),
		Method:        "GET",
		Path:          "/v1/executions",
		Username:      "alice",
		StatusCode:    200,
		Action:        new("get_pods"),
		ExecutionID:   new("1a2cc9ec-fac0-43eb-ba2b-b3f1124f6aea"),
		Jira:          new("JIRA-1"),
		ApprovalState: new("not_required"),
		TargetCluster: new("cluster-1"),
	}

	out := a.ToOpenAPI()

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Id", out.Id, a.ID},
		{"Method", out.Method, openapi.AuditEntryMethod(a.Method)},
		{"Path", out.Path, a.Path},
		{"Username", out.Username, a.Username},
		{"StatusCode", out.StatusCode, a.StatusCode},
		{"Action", *out.Action, *a.Action},
		{"ExecutionId", *out.ExecutionId, *a.ExecutionID},
		{"Jira", *out.Jira, *a.Jira},
		{"ApprovalState", *out.ApprovalState, *a.ApprovalState},
		{"TargetCluster", *out.TargetCluster, *a.TargetCluster},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	if !out.Timestamp.Equal(a.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", out.Timestamp, a.Timestamp)
	}
}

func TestAuditEntry_ToOpenAPI_NilOptionals(t *testing.T) {
	a := &AuditEntry{
		ID:         uuid.New(),
		Timestamp:  time.Now().UTC(),
		Method:     "GET",
		Path:       "/v1/executions",
		Username:   "alice",
		StatusCode: 200,
	}

	out := a.ToOpenAPI()

	if out.Action != nil {
		t.Errorf("Action = %v, want nil", out.Action)
	}
	if out.ExecutionId != nil {
		t.Errorf("ExecutionId = %v, want nil", out.ExecutionId)
	}
	if out.Jira != nil {
		t.Errorf("Jira = %v, want nil", out.Jira)
	}
	if out.ApprovalState != nil {
		t.Errorf("ApprovalState = %v, want nil", out.ApprovalState)
	}
	if out.TargetCluster != nil {
		t.Errorf("TargetCluster = %v, want nil", out.TargetCluster)
	}
}

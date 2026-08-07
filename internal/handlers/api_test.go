package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/types"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
	"github.com/openshift-online/rosa-trusted-actions/internal/store"
)

func newTestHandler(t *testing.T) *APIHandler {
	t.Helper()
	s, err := store.NewSQLiteStore(":memory:", logrus.New())
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewAPIHandler(logrus.New(), s)
}

func TestAPIHandler_Catalog(t *testing.T) {
	handler := newTestHandler(t)

	req := httptest.NewRequest("GET", "/api/v0/trusted-actions/", nil)
	w := httptest.NewRecorder()

	handler.Catalog(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var catalog openapi.TrustedActionCatalog
	if err := json.Unmarshal(w.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if catalog.Total != 2 {
		t.Errorf("Expected 2 actions, got %d", catalog.Total)
	}

	if len(catalog.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(catalog.Items))
	}
}

func TestAPIHandler_Describe(t *testing.T) {
	handler := newTestHandler(t)

	req := httptest.NewRequest("GET", "/api/v0/trusted-actions/cluster-info", nil)
	w := httptest.NewRecorder()

	handler.Describe(w, req, "cluster-info")

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var action openapi.TrustedAction
	if err := json.Unmarshal(w.Body.Bytes(), &action); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if action.Name != "cluster-info" {
		t.Errorf("Expected action name 'cluster-info', got %s", action.Name)
	}

	if action.Type != openapi.Read {
		t.Errorf("Expected action type 'read', got %s", action.Type)
	}
}

func TestAPIHandler_CreateExecution(t *testing.T) {
	handler := newTestHandler(t)

	requestBody := `{
		"target_cluster": "test-cluster",
		"jira": "ROSAENG-1234",
		"params": {"namespace": "default"},
		"dry_run": true
	}`

	req := httptest.NewRequest("POST", "/api/v0/trusted-actions/cluster-info/run", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateExecution(w, req, "cluster-info")

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected status 202, got %d", w.Code)
	}

	var execution openapi.Execution
	if err := json.Unmarshal(w.Body.Bytes(), &execution); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if execution.Action != "cluster-info" {
		t.Errorf("Expected action 'cluster-info', got %s", execution.Action)
	}

	if execution.Status != openapi.ExecutionStatusPending {
		t.Errorf("Expected status 'pending', got %s", execution.Status)
	}

	if execution.TargetCluster != "test-cluster" {
		t.Errorf("Expected target cluster 'test-cluster', got %s", execution.TargetCluster)
	}
}

func TestAPIHandler_CreateExecution_InvalidJSON(t *testing.T) {
	handler := newTestHandler(t)

	requestBody := `{"invalid": json}`

	req := httptest.NewRequest("POST", "/api/v0/trusted-actions/cluster-info/run", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateExecution(w, req, "cluster-info")

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	var errorResp openapi.Error
	if err := json.Unmarshal(w.Body.Bytes(), &errorResp); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	if errorResp.Kind != openapi.ErrorKindError {
		t.Errorf("Expected error kind 'Error', got %s", errorResp.Kind)
	}
}

func TestAPIHandler_GetExecution_NotFound(t *testing.T) {
	handler := newTestHandler(t)

	req := httptest.NewRequest("GET", "/api/v0/trusted-actions/runs/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()

	handler.GetExecution(w, req, types.UUID(uuid.New()), openapi.GetExecutionParams{})

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestAPIHandler_GetExecution_Found(t *testing.T) {
	handler := newTestHandler(t)

	requestBody := `{
		"target_cluster": "test-cluster",
		"jira": "ROSAENG-1234"
	}`
	createReq := httptest.NewRequest("POST", "/api/v0/trusted-actions/cluster-info/run", strings.NewReader(requestBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	handler.CreateExecution(createW, createReq, "cluster-info")

	var created openapi.Execution
	json.Unmarshal(createW.Body.Bytes(), &created)

	getReq := httptest.NewRequest("GET", "/api/v0/trusted-actions/runs/"+created.Id.String(), nil)
	getW := httptest.NewRecorder()
	handler.GetExecution(getW, getReq, created.Id, openapi.GetExecutionParams{})

	if getW.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", getW.Code)
	}

	var got openapi.Execution
	if err := json.Unmarshal(getW.Body.Bytes(), &got); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if got.Id != created.Id {
		t.Errorf("Expected ID %s, got %s", created.Id, got.Id)
	}
	if got.Action != "cluster-info" {
		t.Errorf("Expected action 'cluster-info', got %s", got.Action)
	}
}

func TestAPIHandler_ListExecutions_Empty(t *testing.T) {
	handler := newTestHandler(t)

	req := httptest.NewRequest("GET", "/api/v0/trusted-actions/runs", nil)
	w := httptest.NewRecorder()

	handler.ListExecutions(w, req, openapi.ListExecutionsParams{})

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var list openapi.ExecutionList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if list.Total != 0 {
		t.Errorf("Expected total 0, got %d", list.Total)
	}
	if len(list.Items) != 0 {
		t.Errorf("Expected 0 items, got %d", len(list.Items))
	}
}

func TestAPIHandler_ListExecutions_WithResults(t *testing.T) {
	handler := newTestHandler(t)

	for _, cluster := range []string{"cluster-1", "cluster-2"} {
		body := `{"target_cluster": "` + cluster + `", "jira": "ROSAENG-1234"}`
		req := httptest.NewRequest("POST", "/api/v0/trusted-actions/cluster-info/run", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.CreateExecution(w, req, "cluster-info")
	}

	req := httptest.NewRequest("GET", "/api/v0/trusted-actions/runs", nil)
	w := httptest.NewRecorder()
	handler.ListExecutions(w, req, openapi.ListExecutionsParams{})

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var list openapi.ExecutionList
	json.Unmarshal(w.Body.Bytes(), &list)

	if list.Total != 2 {
		t.Errorf("Expected total 2, got %d", list.Total)
	}
	if len(list.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(list.Items))
	}
}

func TestAPIHandler_ListAuditEntries_Empty(t *testing.T) {
	handler := newTestHandler(t)

	req := httptest.NewRequest("GET", "/api/v0/trusted-actions/audit", nil)
	w := httptest.NewRecorder()

	handler.ListAuditEntries(w, req, openapi.ListAuditEntriesParams{})

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var list openapi.AuditList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if list.Total != 0 {
		t.Errorf("Expected total 0, got %d", list.Total)
	}
}

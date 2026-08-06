package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

func TestAPIHandler_Catalog(t *testing.T) {
	// Create test handler
	logger := logrus.New()
	handler := NewAPIHandler(logger)

	// Create test request
	req := httptest.NewRequest("GET", "/api/v0/trusted-actions/", nil)
	w := httptest.NewRecorder()

	// Call handler
	handler.Catalog(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Parse response
	var catalog openapi.TrustedActionCatalog
	if err := json.Unmarshal(w.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify response content
	if catalog.Total != 2 {
		t.Errorf("Expected 2 actions, got %d", catalog.Total)
	}

	if len(catalog.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(catalog.Items))
	}
}

func TestAPIHandler_Describe(t *testing.T) {
	// Create test handler
	logger := logrus.New()
	handler := NewAPIHandler(logger)

	// Create test request
	req := httptest.NewRequest("GET", "/api/v0/trusted-actions/cluster-info", nil)
	w := httptest.NewRecorder()

	// Call handler with action parameter
	handler.Describe(w, req, "cluster-info")

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Parse response
	var action openapi.TrustedAction
	if err := json.Unmarshal(w.Body.Bytes(), &action); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify response content
	if action.Name != "cluster-info" {
		t.Errorf("Expected action name 'cluster-info', got %s", action.Name)
	}

	if action.Type != openapi.Read {
		t.Errorf("Expected action type 'read', got %s", action.Type)
	}
}

func TestAPIHandler_CreateExecution(t *testing.T) {
	// Create test handler
	logger := logrus.New()
	handler := NewAPIHandler(logger)

	// Create test request body
	requestBody := `{
		"target_cluster": "test-cluster",
		"params": {"namespace": "default"},
		"dry_run": true
	}`

	// Create test request
	req := httptest.NewRequest("POST", "/api/v0/trusted-actions/cluster-info/run", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Call handler with action parameter
	handler.CreateExecution(w, req, "cluster-info")

	// Check response
	if w.Code != http.StatusAccepted {
		t.Errorf("Expected status 202, got %d", w.Code)
	}

	// Parse response
	var execution openapi.Execution
	if err := json.Unmarshal(w.Body.Bytes(), &execution); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify response content
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
	// Create test handler
	logger := logrus.New()
	handler := NewAPIHandler(logger)

	// Create invalid JSON request body
	requestBody := `{"invalid": json}`

	// Create test request
	req := httptest.NewRequest("POST", "/api/v0/trusted-actions/cluster-info/run", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Call handler with action parameter
	handler.CreateExecution(w, req, "cluster-info")

	// Check response
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	// Parse error response
	var errorResp openapi.Error
	if err := json.Unmarshal(w.Body.Bytes(), &errorResp); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	// Verify error response
	if errorResp.Kind != openapi.ErrorKindError {
		t.Errorf("Expected error kind 'Error', got %s", errorResp.Kind)
	}
}

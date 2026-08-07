package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/auth"
	"github.com/openshift-online/rosa-trusted-actions/internal/models"
	"github.com/openshift-online/rosa-trusted-actions/internal/store"
)

func newTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	s, err := store.NewSQLiteStore(context.Background(), ":memory:", logrus.New())
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("failed to close test store: %v", err)
		}
	})
	return s
}

func TestAuditLogger_RecordsBasicFields(t *testing.T) {
	s := newTestStore(t)
	logger := logrus.New()

	handler := NewAuditLogger(s, logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v0/trusted-actions/", nil)
	ctx := auth.SetCallerIdentityContext(req.Context(), &auth.CallerIdentity{Username: "test-user"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	result, err := s.ListAuditEntries(context.Background(), store.AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEntries failed: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("expected 1 audit entry, got %d", result.Total)
	}

	entry := result.Items[0]
	if entry.Method != "GET" {
		t.Errorf("Method: got %s, want GET", entry.Method)
	}
	if entry.Username != "test-user" {
		t.Errorf("Username: got %s, want test-user", entry.Username)
	}
	if entry.StatusCode != http.StatusOK {
		t.Errorf("StatusCode: got %d, want %d", entry.StatusCode, http.StatusOK)
	}
	if entry.ID == uuid.Nil {
		t.Error("ID should not be nil")
	}
}

func TestAuditLogger_CapturesActionFromURL(t *testing.T) {
	s := newTestStore(t)
	logger := logrus.New()

	r := chi.NewRouter()
	r.Route("/{action}", func(r chi.Router) {
		r.Use(NewAuditLogger(s, logger))
		r.Post("/run", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		})
	})

	req := httptest.NewRequest("POST", "/get/run", nil)
	ctx := auth.SetCallerIdentityContext(req.Context(), &auth.CallerIdentity{Username: "srep-user"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	result, err := s.ListAuditEntries(context.Background(), store.AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEntries failed: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("expected 1 audit entry, got %d", result.Total)
	}

	entry := result.Items[0]
	if entry.Action == nil || *entry.Action != "get" {
		t.Errorf("Action: got %v, want 'get'", entry.Action)
	}
	if entry.StatusCode != http.StatusAccepted {
		t.Errorf("StatusCode: got %d, want %d", entry.StatusCode, http.StatusAccepted)
	}
}

func TestAuditLogger_CapturesHandlerEnrichment(t *testing.T) {
	s := newTestStore(t)
	logger := logrus.New()
	ctx := context.Background()

	exec := &models.Execution{
		ID:            uuid.New(),
		Action:        "get",
		Status:        "pending",
		TargetCluster: "prod-cluster",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := s.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("CreateExecution failed: %v", err)
	}
	execID := exec.ID.String()

	handler := NewAuditLogger(s, logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ac := GetAuditContext(r.Context()); ac != nil {
			ac.ExecutionID = execID
			ac.TargetCluster = "prod-cluster"
			ac.Jira = "ROSAENG-999"
			ac.ApprovalState = "approved"
		}
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest("POST", "/api/v0/trusted-actions/get/run", nil)
	reqCtx := auth.SetCallerIdentityContext(req.Context(), &auth.CallerIdentity{Username: "test-user"})
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	result, err := s.ListAuditEntries(ctx, store.AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEntries failed: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("expected 1 audit entry, got %d", result.Total)
	}

	entry := result.Items[0]
	if entry.ExecutionID == nil || *entry.ExecutionID != execID {
		t.Errorf("ExecutionID: got %v, want %s", entry.ExecutionID, execID)
	}
	if entry.TargetCluster == nil || *entry.TargetCluster != "prod-cluster" {
		t.Errorf("TargetCluster: got %v, want prod-cluster", entry.TargetCluster)
	}
	if entry.Jira == nil || *entry.Jira != "ROSAENG-999" {
		t.Errorf("Jira: got %v, want ROSAENG-999", entry.Jira)
	}
	if entry.ApprovalState == nil || *entry.ApprovalState != "approved" {
		t.Errorf("ApprovalState: got %v, want approved", entry.ApprovalState)
	}
}

func TestAuditLogger_NoIdentity(t *testing.T) {
	s := newTestStore(t)
	logger := logrus.New()

	handler := NewAuditLogger(s, logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))

	req := httptest.NewRequest("GET", "/api/v0/trusted-actions/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	result, err := s.ListAuditEntries(context.Background(), store.AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEntries failed: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("expected 1 audit entry, got %d", result.Total)
	}

	entry := result.Items[0]
	if entry.Username != "" {
		t.Errorf("Username: got %s, want empty", entry.Username)
	}
	if entry.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode: got %d, want %d", entry.StatusCode, http.StatusUnauthorized)
	}
}

// Verify that the audit entry has a non-empty ID field (which would fail if
// models.AuditEntry.ID was left as uuid.Nil by accident).
func TestAuditLogger_GeneratesUniqueIDs(t *testing.T) {
	s := newTestStore(t)
	logger := logrus.New()

	handler := NewAuditLogger(s, logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	result, err := s.ListAuditEntries(context.Background(), store.AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEntries failed: %v", err)
	}
	if result.Total != 3 {
		t.Fatalf("expected 3 entries, got %d", result.Total)
	}

	seen := make(map[uuid.UUID]bool)
	for _, e := range result.Items {
		if seen[e.ID] {
			t.Errorf("duplicate audit entry ID: %s", e.ID)
		}
		seen[e.ID] = true
	}
}

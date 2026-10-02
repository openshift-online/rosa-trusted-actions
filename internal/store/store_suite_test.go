package store

// storeTestSuite runs the full behavioural contract of the Store interface
// against any implementation. Call it from both sqlite_test.go and
// postgres_test.go with the appropriate Store instance.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"

	"github.com/openshift-online/rosa-trusted-actions/internal/models"
)

// testExecution builds a minimal valid Execution for use in tests.
func testExecution(action, target string) *models.Execution {
	now := time.Now().UTC().Truncate(time.Microsecond)
	approvalState := "not_required"
	username := "test-user"
	jira := "ROSAENG-1234"
	return &models.Execution{
		ID:            uuid.New(),
		Action:        action,
		Status:        "pending",
		ApprovalState: &approvalState,
		Username:      &username,
		TargetCluster: target,
		Jira:          &jira,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// runStoreTestSuite exercises every Store method. factory is called once per
// subtest so each subtest receives a fresh, empty store; teardown is
// registered via t.Cleanup inside the factory.
func runStoreTestSuite(t *testing.T, factory func(*testing.T) Store) {
	t.Helper()
	t.Run("CreateExecution", func(t *testing.T) { testCreateExecution(t, factory(t)) })
	t.Run("GetExecution_NotFound", func(t *testing.T) { testGetExecutionNotFound(t, factory(t)) })
	t.Run("GetExecutionOutput_NotFound", func(t *testing.T) { testGetExecutionOutputNotFound(t, factory(t)) })
	t.Run("ListExecutions_NoFilter", func(t *testing.T) { testListExecutionsNoFilter(t, factory(t)) })
	t.Run("ListExecutions_FilterByStatus", func(t *testing.T) { testListExecutionsFilterByStatus(t, factory(t)) })
	t.Run("ListExecutions_FilterByAction", func(t *testing.T) { testListExecutionsFilterByAction(t, factory(t)) })
	t.Run("ListExecutions_Limit", func(t *testing.T) { testListExecutionsLimit(t, factory(t)) })
	t.Run("ListExecutions_Since", func(t *testing.T) { testListExecutionsSince(t, factory(t)) })
	t.Run("UpdateExecutionStatus", func(t *testing.T) { testUpdateExecutionStatus(t, factory(t)) })
	t.Run("UpdateExecutionStatusAndOutput", func(t *testing.T) { testUpdateExecutionStatusAndOutput(t, factory(t)) })
	t.Run("UpdateExecutionStatus_NotFound", func(t *testing.T) { testUpdateExecutionStatusNotFound(t, factory(t)) })
	t.Run("ClaimNextExecution", func(t *testing.T) { testClaimNextExecution(t, factory(t)) })
	t.Run("ClaimNextExecution_NotFound", func(t *testing.T) { testClaimNextExecutionNotFound(t, factory(t)) })
	t.Run("CreateAuditEntry", func(t *testing.T) { testCreateAuditEntry(t, factory(t)) })
	t.Run("ListAuditEntries_FilterByAction", func(t *testing.T) { testListAuditEntriesFilterByAction(t, factory(t)) })
	t.Run("ListAuditEntries_FilterByMethod", func(t *testing.T) { testListAuditEntriesFilterByMethod(t, factory(t)) })
	t.Run("AuditEntry_ForeignKeyConstraint", func(t *testing.T) { testAuditEntryForeignKeyConstraint(t, factory(t)) })
	t.Run("AuditEntry_ValidForeignKey", func(t *testing.T) { testAuditEntryValidForeignKey(t, factory(t)) })
	t.Run("ListAuditEntries_Since", func(t *testing.T) { testListAuditEntriesSince(t, factory(t)) })
}

func testCreateExecution(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	exec := testExecution("cluster-info", "test-cluster")
	params := json.RawMessage(`{"namespace":"default"}`)
	exec.Params = &params
	dryRun := true
	exec.DryRun = &dryRun

	if err := s.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("CreateExecution failed: %v", err)
	}

	got, err := s.GetExecution(ctx, exec.ID)
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if got.ID != exec.ID {
		t.Errorf("ID: got %v, want %v", got.ID, exec.ID)
	}
	if got.Action != exec.Action {
		t.Errorf("Action: got %v, want %v", got.Action, exec.Action)
	}
	if got.Status != exec.Status {
		t.Errorf("Status: got %v, want %v", got.Status, exec.Status)
	}
	if got.TargetCluster != exec.TargetCluster {
		t.Errorf("TargetCluster: got %v, want %v", got.TargetCluster, exec.TargetCluster)
	}
	if got.DryRun == nil || !*got.DryRun {
		t.Errorf("DryRun: got %v, want true", got.DryRun)
	}
	if got.Params == nil {
		t.Fatal("Params: got nil, want non-nil")
	}
	if string(*got.Params) != `{"namespace":"default"}` {
		t.Errorf("Params: got %s, want %s", string(*got.Params), `{"namespace":"default"}`)
	}

	out, err := s.GetExecutionOutput(ctx, exec.ID)
	if err == nil {
		t.Errorf("GetExecutionOutput: expected error (no output), got nil")
	}
	if out != nil {
		t.Errorf("GetExecutionOutput: got %v, want nil", out)
	}
}

func testGetExecutionNotFound(t *testing.T, s Store) {
	t.Helper()
	_, err := s.GetExecution(context.Background(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func testGetExecutionOutputNotFound(t *testing.T, s Store) {
	t.Helper()
	_, err := s.GetExecutionOutput(context.Background(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func testListExecutionsNoFilter(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	exec1 := testExecution("cluster-info", "cluster-1")
	exec2 := testExecution("pod-restart", "cluster-2")
	exec2.CreatedAt = exec2.CreatedAt.Add(time.Second)
	exec2.UpdatedAt = exec2.UpdatedAt.Add(time.Second)

	if err := s.CreateExecution(ctx, exec1); err != nil {
		t.Fatalf("CreateExecution exec1 failed: %v", err)
	}
	if err := s.CreateExecution(ctx, exec2); err != nil {
		t.Fatalf("CreateExecution exec2 failed: %v", err)
	}

	result, err := s.ListExecutions(ctx, ExecutionFilter{})
	if err != nil {
		t.Fatalf("ListExecutions failed: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("Total: got %d, want 2", result.Total)
	}
	if len(result.Items) != 2 {
		t.Errorf("Items: got %d, want 2", len(result.Items))
	}
	if result.Items[0].Action != "pod-restart" {
		t.Errorf("first item should be pod-restart (newest), got %s", result.Items[0].Action)
	}
}

func testListExecutionsFilterByStatus(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	exec1 := testExecution("cluster-info", "cluster-1")
	exec2 := testExecution("pod-restart", "cluster-2")
	exec2.Status = "running"

	if err := s.CreateExecution(ctx, exec1); err != nil {
		t.Fatalf("CreateExecution exec1 failed: %v", err)
	}
	if err := s.CreateExecution(ctx, exec2); err != nil {
		t.Fatalf("CreateExecution exec2 failed: %v", err)
	}

	status := "running"
	result, err := s.ListExecutions(ctx, ExecutionFilter{Status: &status})
	if err != nil {
		t.Fatalf("ListExecutions failed: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("Total: got %d, want 1", result.Total)
	}
	if result.Items[0].Action != "pod-restart" {
		t.Errorf("Action: got %s, want pod-restart", result.Items[0].Action)
	}
}

func testListExecutionsFilterByAction(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	if err := s.CreateExecution(ctx, testExecution("cluster-info", "cluster-1")); err != nil {
		t.Fatalf("CreateExecution cluster-info failed: %v", err)
	}
	if err := s.CreateExecution(ctx, testExecution("pod-restart", "cluster-2")); err != nil {
		t.Fatalf("CreateExecution pod-restart failed: %v", err)
	}

	action := "cluster-info"
	result, err := s.ListExecutions(ctx, ExecutionFilter{Action: &action})
	if err != nil {
		t.Fatalf("ListExecutions failed: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("Total: got %d, want 1", result.Total)
	}
}

func testListExecutionsLimit(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		exec := testExecution("cluster-info", "cluster-1")
		exec.CreatedAt = exec.CreatedAt.Add(time.Duration(i) * time.Second)
		exec.UpdatedAt = exec.UpdatedAt.Add(time.Duration(i) * time.Second)
		if err := s.CreateExecution(ctx, exec); err != nil {
			t.Fatalf("CreateExecution[%d] failed: %v", i, err)
		}
	}

	result, err := s.ListExecutions(ctx, ExecutionFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListExecutions failed: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("Total: got %d, want 5", result.Total)
	}
	if len(result.Items) != 2 {
		t.Errorf("Items: got %d, want 2", len(result.Items))
	}
}

func testUpdateExecutionStatus(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	exec := testExecution("cluster-info", "cluster-1")
	if err := s.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("CreateExecution failed: %v", err)
	}

	completedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := s.UpdateExecutionWithResult(ctx, exec.ID, "succeeded", &completedAt, nil); err != nil {
		t.Fatalf("UpdateExecutionWithResult failed: %v", err)
	}

	got, err := s.GetExecution(ctx, exec.ID)
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if got.Status != "succeeded" {
		t.Errorf("Status: got %s, want succeeded", got.Status)
	}
	if got.CompletedAt == nil {
		t.Fatal("CompletedAt: got nil, want non-nil")
	}

	out, err := s.GetExecutionOutput(ctx, exec.ID)
	if err == nil {
		t.Errorf("GetExecutionOutput: expected error, got nil")
	}
	if out != nil {
		t.Errorf("ExecutionOutput: got %v, want nil", out)
	}
}

func testUpdateExecutionStatusAndOutput(t *testing.T, s Store) {
	t.Helper()
	g := NewWithT(t)
	ctx := context.Background()

	exec := testExecution("cluster-info", "cluster-1")
	g.Expect(s.CreateExecution(ctx, exec)).To(Succeed())

	completedAt := time.Now().UTC().Truncate(time.Microsecond)
	resources := []map[string]interface{}{
		{"name": "object1", "data": "some data", "some-key": "some value"},
		{"name": "object2", "data": "some other data", "secret": "can't say"},
	}
	g.Expect(s.UpdateExecutionWithResult(ctx, exec.ID, "succeeded", &completedAt, &models.ExecutionOutput{
		Message:   "execution logs",
		Resources: resources,
	})).To(Succeed())

	got, err := s.GetExecution(ctx, exec.ID)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(got.Status).To(Equal("succeeded"))
	g.Expect(got.CompletedAt).ToNot(BeNil())

	out, err := s.GetExecutionOutput(ctx, exec.ID)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out.Message).To(Equal("execution logs"))
	g.Expect(out.Resources).To(Equal(resources))
}

func testUpdateExecutionStatusNotFound(t *testing.T, s Store) {
	t.Helper()
	err := s.UpdateExecutionWithResult(context.Background(), uuid.New(), "succeeded", nil, nil)
	if !strings.Contains(err.Error(), ErrNotFound.Error()) {
		t.Errorf("expected ErrNotFound in error chain, got %v", err)
	}
}

func testClaimNextExecution(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	older := testExecution("cluster-info", "cluster-1")
	newer := testExecution("pod-restart", "cluster-2")
	newer.CreatedAt = newer.CreatedAt.Add(time.Second)
	newer.UpdatedAt = newer.UpdatedAt.Add(time.Second)

	if err := s.CreateExecution(ctx, newer); err != nil {
		t.Fatalf("CreateExecution newer failed: %v", err)
	}
	if err := s.CreateExecution(ctx, older); err != nil {
		t.Fatalf("CreateExecution older failed: %v", err)
	}

	claimed, err := s.ClaimNextExecution(ctx)
	if err != nil {
		t.Fatalf("ClaimNextExecution failed: %v", err)
	}
	if claimed.ID != older.ID {
		t.Errorf("expected to claim oldest execution (%s), got %s", older.ID, claimed.ID)
	}
	if claimed.Status != "running" {
		t.Errorf("Status: got %s, want running", claimed.Status)
	}

	got, err := s.GetExecution(ctx, older.ID)
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if got.Status != "running" {
		t.Errorf("persisted status: got %s, want running", got.Status)
	}

	claimed2, err := s.ClaimNextExecution(ctx)
	if err != nil {
		t.Fatalf("second ClaimNextExecution failed: %v", err)
	}
	if claimed2.ID != newer.ID {
		t.Errorf("expected to claim remaining pending execution (%s), got %s", newer.ID, claimed2.ID)
	}
}

func testClaimNextExecutionNotFound(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	if _, err := s.ClaimNextExecution(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	exec := testExecution("cluster-info", "cluster-1")
	exec.Status = "running"
	if err := s.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("CreateExecution failed: %v", err)
	}
	if _, err := s.ClaimNextExecution(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound (only running execution exists), got %v", err)
	}
}

func testCreateAuditEntry(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	entry := &models.AuditEntry{
		ID:         uuid.New(),
		Timestamp:  time.Now().UTC().Truncate(time.Microsecond),
		Method:     "POST",
		Path:       "/api/v0/trusted-actions/cluster-info/run",
		Username:   "test-user",
		StatusCode: 202,
	}
	if err := s.CreateAuditEntry(ctx, entry); err != nil {
		t.Fatalf("CreateAuditEntry failed: %v", err)
	}

	result, err := s.ListAuditEntries(ctx, AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEntries failed: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("Total: got %d, want 1", result.Total)
	}
	if result.Items[0].ID != entry.ID {
		t.Errorf("ID: got %v, want %v", result.Items[0].ID, entry.ID)
	}
	if result.Items[0].Method != "POST" {
		t.Errorf("Method: got %s, want POST", result.Items[0].Method)
	}
}

func testListAuditEntriesFilterByAction(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	action1, action2 := "cluster-info", "pod-restart"
	for _, action := range []string{action1, action2} {
		a := action
		if err := s.CreateAuditEntry(ctx, &models.AuditEntry{
			ID: uuid.New(), Timestamp: time.Now().UTC(), Method: "POST",
			Path: "/run", Username: "user1", StatusCode: 202, Action: &a,
		}); err != nil {
			t.Fatalf("CreateAuditEntry %s failed: %v", action, err)
		}
	}

	result, err := s.ListAuditEntries(ctx, AuditFilter{Action: &action1})
	if err != nil {
		t.Fatalf("ListAuditEntries failed: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("Total: got %d, want 1", result.Total)
	}
}

func testListAuditEntriesFilterByMethod(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	for _, method := range []string{"POST", "GET"} {
		m := method
		if err := s.CreateAuditEntry(ctx, &models.AuditEntry{
			ID: uuid.New(), Timestamp: time.Now().UTC(), Method: m,
			Path: "/run", Username: "user1", StatusCode: 200,
		}); err != nil {
			t.Fatalf("CreateAuditEntry %s failed: %v", method, err)
		}
	}

	method := "GET"
	result, err := s.ListAuditEntries(ctx, AuditFilter{Method: &method})
	if err != nil {
		t.Fatalf("ListAuditEntries failed: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("Total: got %d, want 1", result.Total)
	}
}

func testAuditEntryForeignKeyConstraint(t *testing.T, s Store) {
	t.Helper()
	bogusExecID := uuid.New().String()
	err := s.CreateAuditEntry(context.Background(), &models.AuditEntry{
		ID: uuid.New(), Timestamp: time.Now().UTC(), Method: "POST",
		Path: "/run", Username: "user1", StatusCode: 202, ExecutionID: &bogusExecID,
	})
	if err == nil {
		t.Error("expected foreign key violation for non-existent execution_id, got nil")
	}
}

func testAuditEntryValidForeignKey(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	exec := testExecution("get", "cluster-1")
	if err := s.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("CreateExecution failed: %v", err)
	}

	execID := exec.ID.String()
	if err := s.CreateAuditEntry(ctx, &models.AuditEntry{
		ID: uuid.New(), Timestamp: time.Now().UTC(), Method: "POST",
		Path: "/run", Username: "user1", StatusCode: 202, ExecutionID: &execID,
	}); err != nil {
		t.Fatalf("CreateAuditEntry with valid execution_id failed: %v", err)
	}
}

func testListExecutionsSince(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second)

	old := testExecution("cluster-info", "cluster-1")
	old.CreatedAt = base
	old.UpdatedAt = base

	recent := testExecution("pod-restart", "cluster-2")
	recent.CreatedAt = base.Add(2 * time.Second)
	recent.UpdatedAt = base.Add(2 * time.Second)

	for _, exec := range []*models.Execution{old, recent} {
		if err := s.CreateExecution(ctx, exec); err != nil {
			t.Fatalf("CreateExecution failed: %v", err)
		}
	}

	// Filter to only rows at or after base+1s — must include recent, exclude old.
	since := base.Add(time.Second)
	result, err := s.ListExecutions(ctx, ExecutionFilter{Since: &since})
	if err != nil {
		t.Fatalf("ListExecutions with Since failed: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("Total: got %d, want 1 (only the recent execution should be returned)", result.Total)
	}
	if len(result.Items) == 1 && result.Items[0].Action != "pod-restart" {
		t.Errorf("Action: got %s, want pod-restart", result.Items[0].Action)
	}

	// Filter at exactly base — must include both rows.
	since = base
	result, err = s.ListExecutions(ctx, ExecutionFilter{Since: &since})
	if err != nil {
		t.Fatalf("ListExecutions with Since=base failed: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("Total: got %d, want 2 (both executions should be returned)", result.Total)
	}
}

func testListAuditEntriesSince(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second)

	for i, offset := range []time.Duration{0, 2 * time.Second} {
		if err := s.CreateAuditEntry(ctx, &models.AuditEntry{
			ID:         uuid.New(),
			Timestamp:  base.Add(offset),
			Method:     "GET",
			Path:       "/runs",
			Username:   "user1",
			StatusCode: 200,
		}); err != nil {
			t.Fatalf("CreateAuditEntry[%d] failed: %v", i, err)
		}
	}

	// Filter to only rows at or after base+1s — must return 1 entry.
	since := base.Add(time.Second)
	result, err := s.ListAuditEntries(ctx, AuditFilter{Since: &since})
	if err != nil {
		t.Fatalf("ListAuditEntries with Since failed: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("Total: got %d, want 1 (only the recent entry should be returned)", result.Total)
	}

	// Filter at exactly base — must return both entries.
	since = base
	result, err = s.ListAuditEntries(ctx, AuditFilter{Since: &since})
	if err != nil {
		t.Fatalf("ListAuditEntries with Since=base failed: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("Total: got %d, want 2 (both entries should be returned)", result.Total)
	}
}

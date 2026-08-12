package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/models"
	"github.com/openshift-online/rosa-trusted-actions/internal/store"
)

// fakeStore is a minimal store.Store for exercising Pool without SQLite.
type fakeStore struct {
	mu      sync.Mutex
	pending []*models.Execution
}

var _ store.Store = (*fakeStore)(nil)

func (f *fakeStore) push(exec *models.Execution) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = append(f.pending, exec)
}

func (f *fakeStore) ClaimNextExecution(ctx context.Context) (*models.Execution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pending) == 0 {
		return nil, store.ErrNotFound
	}
	exec := f.pending[0]
	f.pending = f.pending[1:]
	return exec, nil
}

func (f *fakeStore) UpdateExecutionStatus(ctx context.Context, id uuid.UUID, status string, completedAt *time.Time) error {
	return nil
}

func (f *fakeStore) CreateExecution(ctx context.Context, exec *models.Execution) error { return nil }

func (f *fakeStore) GetExecution(ctx context.Context, id uuid.UUID) (*models.Execution, error) {
	return nil, store.ErrNotFound
}

func (f *fakeStore) ListExecutions(ctx context.Context, filter store.ExecutionFilter) (*store.ExecutionListResult, error) {
	return &store.ExecutionListResult{}, nil
}

func (f *fakeStore) CreateAuditEntry(ctx context.Context, entry *models.AuditEntry) error { return nil }

func (f *fakeStore) ListAuditEntries(ctx context.Context, filter store.AuditFilter) (*store.AuditListResult, error) {
	return &store.AuditListResult{}, nil
}

func (f *fakeStore) Close() error { return nil }

// fakeRunner records processed executions on a channel so tests can wait for them.
type fakeRunner struct {
	done chan uuid.UUID
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{done: make(chan uuid.UUID, 10)}
}

func (r *fakeRunner) Run(ctx context.Context, exec *models.Execution) (string, *time.Time) {
	r.done <- exec.ID
	now := time.Now().UTC()
	return "succeeded", &now
}

func testExec() *models.Execution {
	return &models.Execution{ID: uuid.New(), Action: "cluster-info", Status: "pending", TargetCluster: "test-cluster"}
}

func TestPool_Notify_WakesWorkerImmediately(t *testing.T) {
	fs := &fakeStore{}
	fr := newFakeRunner()
	// pollInterval intentionally huge: if Notify didn't wake the worker, this
	// test would time out waiting on the poll fallback instead.
	pool := New(fs, logrus.New(), fr, 1, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)

	exec := testExec()
	fs.push(exec)
	pool.Notify()

	select {
	case id := <-fr.done:
		if id != exec.ID {
			t.Errorf("processed execution ID: got %s, want %s", id, exec.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not process execution after Notify")
	}
}

func TestPool_PollFallback_FindsWorkWithoutNotify(t *testing.T) {
	fs := &fakeStore{}
	fr := newFakeRunner()
	pool := New(fs, logrus.New(), fr, 1, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)

	// Push work without ever calling Notify — only the poll ticker should find it.
	exec := testExec()
	fs.push(exec)

	select {
	case id := <-fr.done:
		if id != exec.ID {
			t.Errorf("processed execution ID: got %s, want %s", id, exec.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not find work via poll fallback")
	}
}

func TestPool_Wait_ReturnsAfterCancel(t *testing.T) {
	fs := &fakeStore{}
	fr := newFakeRunner()
	pool := New(fs, logrus.New(), fr, 3, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)
	cancel()

	done := make(chan struct{})
	go func() {
		pool.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after context cancellation")
	}
}

package store

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
)

func newTestSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(context.Background(), ":memory:", logrus.New())
	if err != nil {
		t.Fatalf("failed to create SQLite test store: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("failed to close SQLite test store: %v", err)
		}
	})
	return s
}

// TestSQLiteStore runs the full Store contract against the SQLite implementation.
func TestSQLiteStore(t *testing.T) {
	runStoreTestSuite(t, func(t *testing.T) Store {
		return newTestSQLiteStore(t)
	})
}

// ---------------------------------------------------------------------------
// SQLite-specific: migration mechanics
// ---------------------------------------------------------------------------

func TestSQLiteStore_MigrationsIdempotent(t *testing.T) {
	s := newTestSQLiteStore(t)
	if err := runMigrations(context.Background(), s.db, "sqlite"); err != nil {
		t.Fatalf("running migrations a second time should not error: %v", err)
	}
}

func TestSQLiteStore_RollbackLastMigration(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	if err := RollbackLastMigration(ctx, s.db, "sqlite"); err != nil {
		t.Fatalf("RollbackLastMigration failed: %v", err)
	}

	var count int
	if err := s.db.Get(&count, "SELECT COUNT(*) FROM schema_migrations WHERE version = '001_init'"); err != nil {
		t.Fatalf("querying schema_migrations: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 001_init to be removed from schema_migrations after rollback, got count %d", count)
	}
}

func TestSQLiteStore_RollbackAndReapply(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	var before int
	if err := s.db.Get(&before, "SELECT COUNT(*) FROM schema_migrations"); err != nil {
		t.Fatalf("querying migration count: %v", err)
	}

	if err := RollbackLastMigration(ctx, s.db, "sqlite"); err != nil {
		t.Fatalf("RollbackLastMigration failed: %v", err)
	}

	var during int
	if err := s.db.Get(&during, "SELECT COUNT(*) FROM schema_migrations"); err != nil {
		t.Fatalf("querying migration count after rollback: %v", err)
	}
	if during != before-1 {
		t.Errorf("expected %d migrations after rollback, got %d", before-1, during)
	}

	if err := runMigrations(ctx, s.db, "sqlite"); err != nil {
		t.Fatalf("re-running migrations after rollback failed: %v", err)
	}

	var after int
	if err := s.db.Get(&after, "SELECT COUNT(*) FROM schema_migrations"); err != nil {
		t.Fatalf("querying migration count after reapply: %v", err)
	}
	if after != before {
		t.Errorf("expected %d migrations after reapply, got %d", before, after)
	}
}

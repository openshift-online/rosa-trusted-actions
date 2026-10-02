package store

import (
	"context"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
)

// TestPostgresStore runs the full Store contract against the PostgreSQL
// implementation. It requires a running Postgres instance pointed to by the
// TEST_POSTGRES_URL environment variable and is skipped when that variable is
// absent, keeping the default `go test ./...` fast without Docker.
//
// Example:
//
//	TEST_POSTGRES_URL="postgres://postgres:postgres@localhost:5432/trusted_actions_test?sslmode=disable" \
//	    go test ./internal/store/...
func TestPostgresStore(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set — skipping Postgres integration tests")
	}

	runStoreTestSuite(t, func(t *testing.T) Store {
		t.Helper()
		ctx := context.Background()
		s, err := NewPostgresStore(ctx, dsn, PostgresConfig{
			MaxOpenConns: 5,
			MaxIdleConns: 2,
		}, logrus.New())
		if err != nil {
			t.Fatalf("failed to create PostgresStore: %v", err)
		}
		t.Cleanup(func() {
			_, _ = s.db.ExecContext(ctx, `
				DROP TABLE IF EXISTS audit_entries;
				DROP TABLE IF EXISTS executions_output;
				DROP TABLE IF EXISTS executions;
				DROP TABLE IF EXISTS schema_migrations;
			`)
			if err := s.Close(); err != nil {
				t.Errorf("failed to close PostgresStore: %v", err)
			}
		})
		return s
	})
}

// TestPostgresStore_MigrationsIdempotent verifies that running migrations
// twice against Postgres does not error.
func TestPostgresStore_MigrationsIdempotent(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set — skipping Postgres integration tests")
	}

	ctx := context.Background()
	s, err := NewPostgresStore(ctx, dsn, PostgresConfig{}, logrus.New())
	if err != nil {
		t.Fatalf("NewPostgresStore failed: %v", err)
	}
	defer func() {
		_, _ = s.db.ExecContext(ctx, `
			DROP TABLE IF EXISTS audit_entries;
			DROP TABLE IF EXISTS executions_output;
			DROP TABLE IF EXISTS executions;
			DROP TABLE IF EXISTS schema_migrations;
		`)
		_ = s.Close()
	}()

	if err := runMigrations(ctx, s.db, "postgres"); err != nil {
		t.Fatalf("running migrations a second time should not error: %v", err)
	}
}

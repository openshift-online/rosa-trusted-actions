package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

//go:embed migrations/sqlite migrations/postgres
var migrationsFS embed.FS

// migrationLockKey is the advisory lock key used to serialise concurrent
// Postgres migration runs (e.g. multiple server replicas starting together).
// The value is arbitrary but must be stable across deployments.
const migrationLockKey = 0x526F7361 // "Rosa" in hex

// runMigrations applies any unapplied migrations for the given dialect
// ("sqlite" or "postgres"). Migrations are tracked in a schema_migrations
// table that is created if it does not already exist.
//
// For Postgres, a session-level advisory lock is acquired on a dedicated
// connection before the migration loop and released on return, so that
// concurrent callers (e.g. multiple server replicas) are serialised.
func runMigrations(ctx context.Context, db *sqlx.DB, dialect string) error {
	if dialect == "postgres" {
		conn, err := db.Conn(ctx)
		if err != nil {
			return fmt.Errorf("acquiring migration connection: %w", err)
		}
		defer conn.Close()

		if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
			return fmt.Errorf("acquiring migration advisory lock: %w", err)
		}
		defer conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrationLockKey) //nolint:errcheck
	}

	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	names, err := listMigrations(dialect, ".up.sql")
	if err != nil {
		return err
	}

	checkQ := db.Rebind("SELECT COUNT(*) FROM schema_migrations WHERE version = ?")
	insertQ := db.Rebind("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)")

	for _, name := range names {
		version := strings.TrimSuffix(name, ".up.sql")

		var count int
		if err := db.GetContext(ctx, &count, checkQ, version); err != nil {
			return fmt.Errorf("checking migration %s: %w", version, err)
		}
		if count > 0 {
			continue
		}

		content, err := migrationsFS.ReadFile("migrations/" + dialect + "/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}

		tx, err := db.BeginTxx(ctx, nil)
		if err != nil {
			return fmt.Errorf("beginning transaction for migration %s: %w", version, err)
		}

		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			return errors.Join(fmt.Errorf("applying migration %s: %w", version, err), tx.Rollback())
		}

		if _, err := tx.ExecContext(ctx, insertQ, version, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return errors.Join(fmt.Errorf("recording migration %s: %w", version, err), tx.Rollback())
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %s: %w", version, err)
		}
	}

	return nil
}

func rollbackMigration(ctx context.Context, db *sqlx.DB, dialect, version string) error {
	checkQ := db.Rebind("SELECT COUNT(*) FROM schema_migrations WHERE version = ?")
	var count int
	if err := db.GetContext(ctx, &count, checkQ, version); err != nil {
		return fmt.Errorf("checking migration %s: %w", version, err)
	}
	if count == 0 {
		return fmt.Errorf("migration %s is not applied", version)
	}

	downFile := version + ".down.sql"
	content, err := migrationsFS.ReadFile("migrations/" + dialect + "/" + downFile)
	if err != nil {
		return fmt.Errorf("reading down migration %s: %w", downFile, err)
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction for rollback %s: %w", version, err)
	}

	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return errors.Join(fmt.Errorf("rolling back migration %s: %w", version, err), tx.Rollback())
	}

	deleteQ := db.Rebind("DELETE FROM schema_migrations WHERE version = ?")
	if _, err := tx.ExecContext(ctx, deleteQ, version); err != nil {
		return errors.Join(fmt.Errorf("removing migration record %s: %w", version, err), tx.Rollback())
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing rollback %s: %w", version, err)
	}

	return nil
}

// RollbackLastMigration rolls back the most recently applied migration.
func RollbackLastMigration(ctx context.Context, db *sqlx.DB, dialect string) error {
	var version string
	err := db.GetContext(ctx, &version, "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1")
	if err != nil {
		return fmt.Errorf("finding last migration: %w", err)
	}
	return rollbackMigration(ctx, db, dialect, version)
}

func listMigrations(dialect, suffix string) ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations/" + dialect)
	if err != nil {
		return nil, fmt.Errorf("reading migrations directory for dialect %q: %w", dialect, err)
	}

	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

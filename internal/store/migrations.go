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

//go:embed migrations/*.sql
var migrationsFS embed.FS

func runMigrations(ctx context.Context, db *sqlx.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	names, err := listMigrations(".up.sql")
	if err != nil {
		return err
	}

	for _, name := range names {
		version := strings.TrimSuffix(name, ".up.sql")

		var count int
		err := db.GetContext(ctx, &count, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", version, err)
		}
		if count > 0 {
			continue
		}

		content, err := migrationsFS.ReadFile("migrations/" + name)
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

		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
			version, time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			return errors.Join(fmt.Errorf("recording migration %s: %w", version, err), tx.Rollback())
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %s: %w", version, err)
		}
	}

	return nil
}

func rollbackMigration(ctx context.Context, db *sqlx.DB, version string) error {
	var count int
	if err := db.GetContext(ctx, &count, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version); err != nil {
		return fmt.Errorf("checking migration %s: %w", version, err)
	}
	if count == 0 {
		return fmt.Errorf("migration %s is not applied", version)
	}

	downFile := version + ".down.sql"
	content, err := migrationsFS.ReadFile("migrations/" + downFile)
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

	if _, err := tx.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version = ?", version); err != nil {
		return errors.Join(fmt.Errorf("removing migration record %s: %w", version, err), tx.Rollback())
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing rollback %s: %w", version, err)
	}

	return nil
}

// RollbackLastMigration rolls back the most recently applied migration.
func RollbackLastMigration(ctx context.Context, db *sqlx.DB) error {
	var version string
	err := db.GetContext(ctx, &version, "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1")
	if err != nil {
		return fmt.Errorf("finding last migration: %w", err)
	}
	return rollbackMigration(ctx, db, version)
}

func listMigrations(suffix string) ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("reading migrations directory: %w", err)
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

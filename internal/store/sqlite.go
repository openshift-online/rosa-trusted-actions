package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"

	"github.com/openshift-online/rosa-trusted-actions/internal/models"
)

// timeLayout is a fixed-width RFC3339 layout so SQLite TEXT comparisons sort
// correctly without a native timestamp type.
const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// SQLiteStore implements Store using a SQLite database. Intended for local
// development and testing; use PostgresStore for production deployments.
type SQLiteStore struct {
	db     *sqlx.DB
	logger *logrus.Logger
}

var _ Store = (*SQLiteStore)(nil)

func NewSQLiteStore(ctx context.Context, dsn string, logger *logrus.Logger) (*SQLiteStore, error) {
	if dsn == "" {
		dsn = "trusted_actions.db"
		logger.Warn("DATABASE_URL not set, using local file 'trusted_actions.db' — data will be lost in ephemeral environments")
	}

	db, err := sqlx.Open("sqlite", withSQLitePragmas(dsn, map[string]string{
		"_journal_mode": "WAL",
		"_foreign_keys": "on",
		"_busy_timeout": "5000",
	}))
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// SQLite: single connection avoids "database is locked" on file-backed DBs
	// and prevents :memory: from splitting across pool connections.
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			logger.WithError(closeErr).Warn("Failed to close database after ping failure")
		}
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	if err := runMigrations(ctx, db, "sqlite"); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			logger.WithError(closeErr).Warn("Failed to close database after migration failure")
		}
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	logger.Info("SQLite database initialized")

	return &SQLiteStore{db: db, logger: logger}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) CreateExecution(ctx context.Context, exec *models.Execution) error {
	var paramsStr *string
	if exec.Params != nil {
		p := string(*exec.Params)
		paramsStr = &p
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO executions (
			id, action, status, approval_state, username, target_cluster,
			jira, dry_run, force, params, scope, type, revision,
			manifest_work_name,
			runner_seconds, upload_seconds, duration_seconds,
			created_at, updated_at, completed_at
		) VALUES (
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?,
			?,
			?, ?, ?,
			?, ?, ?
		)`,
		exec.ID.String(), exec.Action, exec.Status, exec.ApprovalState, exec.Username, exec.TargetCluster,
		exec.Jira, exec.DryRun, exec.Force, paramsStr, exec.Scope, exec.Type, exec.Revision,
		exec.ManifestWorkName,
		exec.RunnerSeconds, exec.UploadSeconds, exec.DurationSeconds,
		exec.CreatedAt.UTC().Format(timeLayout), exec.UpdatedAt.UTC().Format(timeLayout),
		formatTimePtr(exec.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("inserting execution: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetExecution(ctx context.Context, id uuid.UUID) (*models.Execution, error) {
	row := s.db.QueryRowxContext(ctx, "SELECT "+executionColumns+" FROM executions WHERE id = ?", id.String())

	var raw sqliteExecutionRow
	if err := row.StructScan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("querying execution: %w", err)
	}

	return raw.toModel()
}

func (s *SQLiteStore) GetExecutionOutput(ctx context.Context, execId uuid.UUID) (*models.ExecutionOutput, error) {
	row := s.db.QueryRowxContext(ctx, "SELECT "+outputColumns+" FROM executions_output WHERE exec_id = ?", execId.String())

	var raw sqliteExecutionOutputRow
	if err := row.StructScan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("querying execution output: %w", err)
	}

	return raw.toModel()
}

func (s *SQLiteStore) ListExecutions(ctx context.Context, filter ExecutionFilter) (*ExecutionListResult, error) {
	where, args := buildExecutionWhere(filter)
	if filter.Since != nil {
		where = append(where, "created_at >= ?")
		args = append(args, filter.Since.UTC().Format(timeLayout))
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM executions %s", whereClause)
	if err := s.db.GetContext(ctx, &total, countQuery, args...); err != nil {
		return nil, fmt.Errorf("counting executions: %w", err)
	}

	limit := clampLimit(filter.Limit, 20, 100)
	offset := clampOffset(filter.Offset)

	query := fmt.Sprintf("SELECT %s FROM executions %s ORDER BY created_at DESC, id ASC LIMIT ? OFFSET ?", executionColumns, whereClause)
	args = append(args, limit, offset)

	var rows []sqliteExecutionRow
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("listing executions: %w", err)
	}

	items := make([]models.Execution, 0, len(rows))
	for _, row := range rows {
		exec, err := row.toModel()
		if err != nil {
			return nil, err
		}
		items = append(items, *exec)
	}

	return &ExecutionListResult{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *SQLiteStore) UpdateExecutionWithResult(ctx context.Context, id uuid.UUID, status string, completedAt *time.Time, output *models.ExecutionOutput) error {
	opts := &sql.TxOptions{Isolation: sql.LevelSerializable}

	tx, err := s.db.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("initiating transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			s.logger.WithError(err).Warn("Failed to rollback transaction")
		}
	}()

	txExec := func(query string, mustAffectRows bool, args ...any) error {
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("updating table: %w", err)
		}
		if mustAffectRows {
			n, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("checking rows affected: %w", err)
			}
			if n == 0 {
				return ErrNotFound
			}
		}
		return nil
	}

	err = txExec(`UPDATE executions SET status = ?, updated_at = ?, completed_at = ? WHERE id = ?`,
		true, status, time.Now().UTC().Format(timeLayout), formatTimePtr(completedAt), id.String())
	if err != nil {
		return fmt.Errorf("updating execution status: %w", err)
	}

	err = txExec(`DELETE FROM executions_output WHERE exec_id = ?`, false, id.String())
	if err != nil {
		return fmt.Errorf("deleting execution output: %w", err)
	}

	if output != nil {
		resourcesData, err := json.Marshal(output.Resources)
		if err != nil {
			return fmt.Errorf("serialising execution output resources: %w", err)
		}

		err = txExec(`INSERT INTO executions_output (`+outputColumns+`) VALUES (?, ?, ?, ?)`,
			true, uuid.New().String(), id.String(), output.Message, string(resourcesData))
		if err != nil {
			return fmt.Errorf("creating execution output: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// ClaimNextExecution atomically claims the oldest pending execution. Safe
// today because SetMaxOpenConns(1) serializes all DB access; see
// PostgresStore.ClaimNextExecution for the multi-connection approach using
// SELECT FOR UPDATE SKIP LOCKED.
func (s *SQLiteStore) ClaimNextExecution(ctx context.Context) (*models.Execution, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var id string
	err = tx.GetContext(ctx, &id,
		"SELECT id FROM executions WHERE status = 'pending' ORDER BY created_at ASC, id ASC LIMIT 1")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("selecting next pending execution: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		"UPDATE executions SET status = 'running', updated_at = ? WHERE id = ?",
		time.Now().UTC().Format(timeLayout), id,
	); err != nil {
		return nil, fmt.Errorf("claiming execution: %w", err)
	}

	row := tx.QueryRowxContext(ctx, "SELECT "+executionColumns+" FROM executions WHERE id = ?", id)
	var raw sqliteExecutionRow
	if err := row.StructScan(&raw); err != nil {
		return nil, fmt.Errorf("querying claimed execution: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing claim transaction: %w", err)
	}

	return raw.toModel()
}

func (s *SQLiteStore) CreateAuditEntry(ctx context.Context, entry *models.AuditEntry) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_entries (
			id, timestamp, method, path, username, status_code,
			action, execution_id, jira, approval_state, target_cluster
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID.String(),
		entry.Timestamp.UTC().Format(timeLayout),
		entry.Method, entry.Path, entry.Username, entry.StatusCode,
		entry.Action, entry.ExecutionID, entry.Jira, entry.ApprovalState, entry.TargetCluster,
	)
	if err != nil {
		return fmt.Errorf("inserting audit entry: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListAuditEntries(ctx context.Context, filter AuditFilter) (*AuditListResult, error) {
	where, args := buildAuditWhere(filter)
	if filter.Since != nil {
		where = append(where, "timestamp >= ?")
		args = append(args, filter.Since.UTC().Format(timeLayout))
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_entries %s", whereClause)
	if err := s.db.GetContext(ctx, &total, countQuery, args...); err != nil {
		return nil, fmt.Errorf("counting audit entries: %w", err)
	}

	limit := clampLimit(filter.Limit, 50, 200)
	offset := clampOffset(filter.Offset)

	query := fmt.Sprintf("SELECT %s FROM audit_entries %s ORDER BY timestamp DESC, id ASC LIMIT ? OFFSET ?", auditColumns, whereClause)
	args = append(args, limit, offset)

	var rows []sqliteAuditRow
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("listing audit entries: %w", err)
	}

	items := make([]models.AuditEntry, 0, len(rows))
	for _, row := range rows {
		entry, err := row.toModel()
		if err != nil {
			return nil, err
		}
		items = append(items, *entry)
	}

	return &AuditListResult{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

// ---------------------------------------------------------------------------
// SQLite-specific row types (timestamps stored as TEXT)
// ---------------------------------------------------------------------------

type sqliteExecutionRow struct {
	ID               string  `db:"id"`
	Action           string  `db:"action"`
	Status           string  `db:"status"`
	ApprovalState    *string `db:"approval_state"`
	Username         *string `db:"username"`
	TargetCluster    string  `db:"target_cluster"`
	Jira             *string `db:"jira"`
	DryRun           *bool   `db:"dry_run"`
	Force            *bool   `db:"force"`
	Params           *string `db:"params"`
	Scope            *string `db:"scope"`
	Type             *string `db:"type"`
	Revision         *string `db:"revision"`
	ManifestWorkName *string `db:"manifest_work_name"`
	RunnerSeconds    *int    `db:"runner_seconds"`
	UploadSeconds    *int    `db:"upload_seconds"`
	DurationSeconds  *int    `db:"duration_seconds"`
	CreatedAt        string  `db:"created_at"`
	UpdatedAt        string  `db:"updated_at"`
	CompletedAt      *string `db:"completed_at"`
}

func (r *sqliteExecutionRow) toModel() (*models.Execution, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("parsing execution id: %w", err)
	}

	createdAt, err := time.Parse(timeLayout, r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}

	updatedAt, err := time.Parse(timeLayout, r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing updated_at: %w", err)
	}

	exec := &models.Execution{
		ID:               id,
		Action:           r.Action,
		Status:           r.Status,
		ApprovalState:    r.ApprovalState,
		Username:         r.Username,
		TargetCluster:    r.TargetCluster,
		Jira:             r.Jira,
		DryRun:           r.DryRun,
		Force:            r.Force,
		Scope:            r.Scope,
		Type:             r.Type,
		Revision:         r.Revision,
		ManifestWorkName: r.ManifestWorkName,
		RunnerSeconds:    r.RunnerSeconds,
		UploadSeconds:    r.UploadSeconds,
		DurationSeconds:  r.DurationSeconds,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}

	if r.Params != nil {
		raw := json.RawMessage(*r.Params)
		exec.Params = &raw
	}

	if r.CompletedAt != nil {
		t, err := time.Parse(timeLayout, *r.CompletedAt)
		if err != nil {
			return nil, fmt.Errorf("parsing completed_at: %w", err)
		}
		exec.CompletedAt = &t
	}

	return exec, nil
}

type sqliteExecutionOutputRow struct {
	ID        string `db:"id"`
	ExecId    string `db:"exec_id"`
	Message   string `db:"message"`
	Resources string `db:"resources"`
}

func (r *sqliteExecutionOutputRow) toModel() (*models.ExecutionOutput, error) {
	var resources []map[string]interface{}
	if err := json.Unmarshal([]byte(r.Resources), &resources); err != nil {
		return nil, fmt.Errorf("parsing execution output resources: %w", err)
	}
	return &models.ExecutionOutput{Message: r.Message, Resources: resources}, nil
}

type sqliteAuditRow struct {
	ID            string  `db:"id"`
	Timestamp     string  `db:"timestamp"`
	Method        string  `db:"method"`
	Path          string  `db:"path"`
	Username      string  `db:"username"`
	StatusCode    int     `db:"status_code"`
	Action        *string `db:"action"`
	ExecutionID   *string `db:"execution_id"`
	Jira          *string `db:"jira"`
	ApprovalState *string `db:"approval_state"`
	TargetCluster *string `db:"target_cluster"`
}

func (r *sqliteAuditRow) toModel() (*models.AuditEntry, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("parsing audit entry id: %w", err)
	}

	ts, err := time.Parse(timeLayout, r.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("parsing timestamp: %w", err)
	}

	return &models.AuditEntry{
		ID:            id,
		Timestamp:     ts,
		Method:        r.Method,
		Path:          r.Path,
		Username:      r.Username,
		StatusCode:    r.StatusCode,
		Action:        r.Action,
		ExecutionID:   r.ExecutionID,
		Jira:          r.Jira,
		ApprovalState: r.ApprovalState,
		TargetCluster: r.TargetCluster,
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func withSQLitePragmas(dsn string, pragmas map[string]string) string {
	sep := "?"
	if strings.ContainsRune(dsn, '?') {
		sep = "&"
	}
	for k, v := range pragmas {
		if !strings.Contains(dsn, k+"=") {
			dsn += sep + k + "=" + v
			sep = "&"
		}
	}
	return dsn
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(timeLayout)
	return &s
}

func clampLimit(v, defaultVal, max int) int {
	if v <= 0 {
		return defaultVal
	}
	if v > max {
		return max
	}
	return v
}

func clampOffset(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

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
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/models"
)

// PostgresStore implements Store using a PostgreSQL database. Suitable for
// local testing against a real Postgres instance and for production deployments
// on AWS RDS or Aurora PostgreSQL.
type PostgresStore struct {
	db     *sqlx.DB
	logger *logrus.Logger
}

var _ Store = (*PostgresStore)(nil)

// PostgresConfig holds pool-tuning settings for the PostgresStore.
type PostgresConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

func NewPostgresStore(ctx context.Context, dsn string, cfg PostgresConfig, logger *logrus.Logger) (*PostgresStore, error) {
	db, err := sqlx.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening postgres database: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}

	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			logger.WithError(closeErr).Warn("Failed to close database after ping failure")
		}
		return nil, fmt.Errorf("pinging postgres database: %w", err)
	}

	if err := runMigrations(ctx, db, "postgres"); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			logger.WithError(closeErr).Warn("Failed to close database after migration failure")
		}
		return nil, fmt.Errorf("running postgres migrations: %w", err)
	}

	logger.Info("PostgreSQL database initialized")

	return &PostgresStore{db: db, logger: logger}, nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) CreateExecution(ctx context.Context, exec *models.Execution) error {
	var paramsStr *string
	if exec.Params != nil {
		p := string(*exec.Params)
		paramsStr = &p
	}

	q := s.db.Rebind(`
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
		)`)

	_, err := s.db.ExecContext(ctx, q,
		exec.ID.String(), exec.Action, exec.Status, exec.ApprovalState, exec.Username, exec.TargetCluster,
		exec.Jira, exec.DryRun, exec.Force, paramsStr, exec.Scope, exec.Type, exec.Revision,
		exec.ManifestWorkName,
		exec.RunnerSeconds, exec.UploadSeconds, exec.DurationSeconds,
		exec.CreatedAt.UTC(), exec.UpdatedAt.UTC(), exec.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting execution: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetExecution(ctx context.Context, id uuid.UUID) (*models.Execution, error) {
	q := s.db.Rebind("SELECT " + executionColumns + " FROM executions WHERE id = ?")
	row := s.db.QueryRowxContext(ctx, q, id.String())

	var raw pgExecutionRow
	if err := row.StructScan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("querying execution: %w", err)
	}

	return raw.toModel()
}

func (s *PostgresStore) GetExecutionOutput(ctx context.Context, execId uuid.UUID) (*models.ExecutionOutput, error) {
	q := s.db.Rebind("SELECT " + outputColumns + " FROM executions_output WHERE exec_id = ?")
	row := s.db.QueryRowxContext(ctx, q, execId.String())

	var raw pgExecutionOutputRow
	if err := row.StructScan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("querying execution output: %w", err)
	}

	return raw.toModel()
}

func (s *PostgresStore) ListExecutions(ctx context.Context, filter ExecutionFilter) (*ExecutionListResult, error) {
	where, args := buildExecutionWhere(filter)
	if filter.Since != nil {
		where = append(where, "created_at >= ?")
		args = append(args, filter.Since.UTC())
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	countQ := s.db.Rebind(fmt.Sprintf("SELECT COUNT(*) FROM executions %s", whereClause))
	var total int
	if err := s.db.GetContext(ctx, &total, countQ, args...); err != nil {
		return nil, fmt.Errorf("counting executions: %w", err)
	}

	limit := clampLimit(filter.Limit, 20, 100)
	offset := clampOffset(filter.Offset)

	q := s.db.Rebind(fmt.Sprintf(
		"SELECT %s FROM executions %s ORDER BY created_at DESC, id ASC LIMIT ? OFFSET ?",
		executionColumns, whereClause))
	args = append(args, limit, offset)

	var rows []pgExecutionRow
	if err := s.db.SelectContext(ctx, &rows, q, args...); err != nil {
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

func (s *PostgresStore) UpdateExecutionWithResult(ctx context.Context, id uuid.UUID, status string, completedAt *time.Time, output *models.ExecutionOutput) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("initiating transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			s.logger.WithError(err).Warn("Failed to rollback transaction")
		}
	}()

	updateQ := s.db.Rebind(`UPDATE executions SET status = ?, updated_at = ?, completed_at = ? WHERE id = ?`)
	result, err := tx.ExecContext(ctx, updateQ, status, time.Now().UTC(), completedAt, id.String())
	if err != nil {
		return fmt.Errorf("updating execution status: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("updating execution status: %w", ErrNotFound)
	}

	deleteQ := s.db.Rebind(`DELETE FROM executions_output WHERE exec_id = ?`)
	if _, err := tx.ExecContext(ctx, deleteQ, id.String()); err != nil {
		return fmt.Errorf("deleting execution output: %w", err)
	}

	if output != nil {
		resourcesData, err := json.Marshal(output.Resources)
		if err != nil {
			return fmt.Errorf("serialising execution output resources: %w", err)
		}

		insertQ := s.db.Rebind(`INSERT INTO executions_output (` + outputColumns + `) VALUES (?, ?, ?, ?)`)
		if _, err := tx.ExecContext(ctx, insertQ, uuid.New().String(), id.String(), output.Message, string(resourcesData)); err != nil {
			return fmt.Errorf("creating execution output: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// ClaimNextExecution atomically claims the oldest pending execution using
// SELECT FOR UPDATE SKIP LOCKED — the idiomatic Postgres pattern for
// queue-style workloads. Multiple concurrent workers can call this safely
// without serialization bottlenecks; each claim only locks the single row
// being transitioned, leaving all other pending rows available.
func (s *PostgresStore) ClaimNextExecution(ctx context.Context) (*models.Execution, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var id string
	err = tx.GetContext(ctx, &id,
		`SELECT id FROM executions
		 WHERE status = 'pending'
		 ORDER BY created_at ASC, id ASC
		 LIMIT 1
		 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("selecting next pending execution: %w", err)
	}

	updateQ := tx.Rebind(`UPDATE executions SET status = 'running', updated_at = ? WHERE id = ?`)
	if _, err := tx.ExecContext(ctx, updateQ, time.Now().UTC(), id); err != nil {
		return nil, fmt.Errorf("claiming execution: %w", err)
	}

	selectQ := tx.Rebind("SELECT " + executionColumns + " FROM executions WHERE id = ?")
	row := tx.QueryRowxContext(ctx, selectQ, id)
	var raw pgExecutionRow
	if err := row.StructScan(&raw); err != nil {
		return nil, fmt.Errorf("querying claimed execution: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing claim transaction: %w", err)
	}

	return raw.toModel()
}

func (s *PostgresStore) CreateAuditEntry(ctx context.Context, entry *models.AuditEntry) error {
	q := s.db.Rebind(`
		INSERT INTO audit_entries (
			id, timestamp, method, path, username, status_code,
			action, execution_id, jira, approval_state, target_cluster
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)

	_, err := s.db.ExecContext(ctx, q,
		entry.ID.String(), entry.Timestamp.UTC(),
		entry.Method, entry.Path, entry.Username, entry.StatusCode,
		entry.Action, entry.ExecutionID, entry.Jira, entry.ApprovalState, entry.TargetCluster,
	)
	if err != nil {
		return fmt.Errorf("inserting audit entry: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListAuditEntries(ctx context.Context, filter AuditFilter) (*AuditListResult, error) {
	where, args := buildAuditWhere(filter)
	if filter.Since != nil {
		where = append(where, "timestamp >= ?")
		args = append(args, filter.Since.UTC())
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	countQ := s.db.Rebind(fmt.Sprintf("SELECT COUNT(*) FROM audit_entries %s", whereClause))
	var total int
	if err := s.db.GetContext(ctx, &total, countQ, args...); err != nil {
		return nil, fmt.Errorf("counting audit entries: %w", err)
	}

	limit := clampLimit(filter.Limit, 50, 200)
	offset := clampOffset(filter.Offset)

	q := s.db.Rebind(fmt.Sprintf(
		"SELECT %s FROM audit_entries %s ORDER BY timestamp DESC, id ASC LIMIT ? OFFSET ?",
		auditColumns, whereClause))
	args = append(args, limit, offset)

	var rows []pgAuditRow
	if err := s.db.SelectContext(ctx, &rows, q, args...); err != nil {
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
// Postgres-specific row types (TIMESTAMPTZ scanned as time.Time, BOOLEAN as bool)
// ---------------------------------------------------------------------------

type pgExecutionRow struct {
	ID               string     `db:"id"`
	Action           string     `db:"action"`
	Status           string     `db:"status"`
	ApprovalState    *string    `db:"approval_state"`
	Username         *string    `db:"username"`
	TargetCluster    string     `db:"target_cluster"`
	Jira             *string    `db:"jira"`
	DryRun           *bool      `db:"dry_run"`
	Force            *bool      `db:"force"`
	Params           *string    `db:"params"`
	Scope            *string    `db:"scope"`
	Type             *string    `db:"type"`
	Revision         *string    `db:"revision"`
	ManifestWorkName *string    `db:"manifest_work_name"`
	RunnerSeconds    *int       `db:"runner_seconds"`
	UploadSeconds    *int       `db:"upload_seconds"`
	DurationSeconds  *int       `db:"duration_seconds"`
	CreatedAt        time.Time  `db:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"`
	CompletedAt      *time.Time `db:"completed_at"`
}

func (r *pgExecutionRow) toModel() (*models.Execution, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("parsing execution id: %w", err)
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
		CreatedAt:        r.CreatedAt.UTC(),
		UpdatedAt:        r.UpdatedAt.UTC(),
		CompletedAt:      r.CompletedAt,
	}

	if r.Params != nil {
		raw := json.RawMessage(*r.Params)
		exec.Params = &raw
	}

	if exec.CompletedAt != nil {
		t := exec.CompletedAt.UTC()
		exec.CompletedAt = &t
	}

	return exec, nil
}

type pgExecutionOutputRow struct {
	ID        string `db:"id"`
	ExecId    string `db:"exec_id"`
	Message   string `db:"message"`
	Resources string `db:"resources"`
}

func (r *pgExecutionOutputRow) toModel() (*models.ExecutionOutput, error) {
	var resources []map[string]interface{}
	if err := json.Unmarshal([]byte(r.Resources), &resources); err != nil {
		return nil, fmt.Errorf("parsing execution output resources: %w", err)
	}
	return &models.ExecutionOutput{Message: r.Message, Resources: resources}, nil
}

type pgAuditRow struct {
	ID            string    `db:"id"`
	Timestamp     time.Time `db:"timestamp"`
	Method        string    `db:"method"`
	Path          string    `db:"path"`
	Username      string    `db:"username"`
	StatusCode    int       `db:"status_code"`
	Action        *string   `db:"action"`
	ExecutionID   *string   `db:"execution_id"`
	Jira          *string   `db:"jira"`
	ApprovalState *string   `db:"approval_state"`
	TargetCluster *string   `db:"target_cluster"`
}

func (r *pgAuditRow) toModel() (*models.AuditEntry, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("parsing audit entry id: %w", err)
	}

	return &models.AuditEntry{
		ID:            id,
		Timestamp:     r.Timestamp.UTC(),
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

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
	_ "github.com/mattn/go-sqlite3"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/models"
)

type SQLiteStore struct {
	db     *sqlx.DB
	logger *logrus.Logger
}

var _ Store = (*SQLiteStore)(nil)

func NewSQLiteStore(dsn string, logger *logrus.Logger) (*SQLiteStore, error) {
	if dsn == "" {
		dsn = "trusted_actions.db"
	}

	db, err := sqlx.Open("sqlite3", dsn+"?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	logger.WithField("dsn", dsn).Info("Database initialized")

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
			manifest_work_name, output_path, output_status,
			runner_seconds, upload_seconds, duration_seconds,
			created_at, updated_at, completed_at
		) VALUES (
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?,
			?, ?, ?,
			?, ?, ?,
			?, ?, ?
		)`,
		exec.ID.String(), exec.Action, exec.Status, exec.ApprovalState, exec.Username, exec.TargetCluster,
		exec.Jira, exec.DryRun, exec.Force, paramsStr, exec.Scope, exec.Type, exec.Revision,
		exec.ManifestWorkName, exec.OutputPath, exec.OutputStatus,
		exec.RunnerSeconds, exec.UploadSeconds, exec.DurationSeconds,
		exec.CreatedAt.UTC().Format(time.RFC3339Nano), exec.UpdatedAt.UTC().Format(time.RFC3339Nano),
		formatTimePtr(exec.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("inserting execution: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetExecution(ctx context.Context, id uuid.UUID) (*models.Execution, error) {
	row := s.db.QueryRowxContext(ctx, "SELECT * FROM executions WHERE id = ?", id.String())

	var raw executionRow
	if err := row.StructScan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("querying execution: %w", err)
	}

	exec, err := raw.toModel()
	if err != nil {
		return nil, err
	}
	return exec, nil
}

func (s *SQLiteStore) ListExecutions(ctx context.Context, filter ExecutionFilter) (*ExecutionListResult, error) {
	where, args := buildExecutionWhere(filter)

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM executions %s", whereClause)
	if err := s.db.GetContext(ctx, &total, countQuery, args...); err != nil {
		return nil, fmt.Errorf("counting executions: %w", err)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	query := fmt.Sprintf("SELECT * FROM executions %s ORDER BY created_at DESC LIMIT ?", whereClause)
	args = append(args, limit)

	var rows []executionRow
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

	return &ExecutionListResult{Items: items, Total: total}, nil
}

func (s *SQLiteStore) UpdateExecutionStatus(ctx context.Context, id uuid.UUID, status string, completedAt *time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE executions SET status = ?, updated_at = ?, completed_at = ? WHERE id = ?`,
		status,
		time.Now().UTC().Format(time.RFC3339Nano),
		formatTimePtr(completedAt),
		id.String(),
	)
	if err != nil {
		return fmt.Errorf("updating execution status: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) CreateAuditEntry(ctx context.Context, entry *models.AuditEntry) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_entries (
			id, timestamp, method, path, username, status_code,
			action, execution_id, jira, approval_state, target_cluster
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID.String(),
		entry.Timestamp.UTC().Format(time.RFC3339Nano),
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

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_entries %s", whereClause)
	if err := s.db.GetContext(ctx, &total, countQuery, args...); err != nil {
		return nil, fmt.Errorf("counting audit entries: %w", err)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	query := fmt.Sprintf("SELECT * FROM audit_entries %s ORDER BY timestamp DESC LIMIT ?", whereClause)
	args = append(args, limit)

	var rows []auditRow
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

	return &AuditListResult{Items: items, Total: total}, nil
}

// executionRow is the raw database representation with TEXT timestamps.
type executionRow struct {
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
	OutputPath       *string `db:"output_path"`
	OutputStatus     *string `db:"output_status"`
	RunnerSeconds    *int    `db:"runner_seconds"`
	UploadSeconds    *int    `db:"upload_seconds"`
	DurationSeconds  *int    `db:"duration_seconds"`
	CreatedAt        string  `db:"created_at"`
	UpdatedAt        string  `db:"updated_at"`
	CompletedAt      *string `db:"completed_at"`
}

func (r *executionRow) toModel() (*models.Execution, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("parsing execution id: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}

	updatedAt, err := time.Parse(time.RFC3339Nano, r.UpdatedAt)
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
		OutputPath:       r.OutputPath,
		OutputStatus:     r.OutputStatus,
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
		t, err := time.Parse(time.RFC3339Nano, *r.CompletedAt)
		if err != nil {
			return nil, fmt.Errorf("parsing completed_at: %w", err)
		}
		exec.CompletedAt = &t
	}

	return exec, nil
}

type auditRow struct {
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

func (r *auditRow) toModel() (*models.AuditEntry, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("parsing audit entry id: %w", err)
	}

	ts, err := time.Parse(time.RFC3339Nano, r.Timestamp)
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

func buildExecutionWhere(filter ExecutionFilter) ([]string, []interface{}) {
	var clauses []string
	var args []interface{}

	if filter.Status != nil {
		clauses = append(clauses, "status = ?")
		args = append(args, *filter.Status)
	}
	if filter.Action != nil {
		clauses = append(clauses, "action = ?")
		args = append(args, *filter.Action)
	}
	if filter.Target != nil {
		clauses = append(clauses, "target_cluster = ?")
		args = append(args, *filter.Target)
	}
	if filter.Operator != nil {
		clauses = append(clauses, "username = ?")
		args = append(args, *filter.Operator)
	}
	if filter.Scope != nil {
		clauses = append(clauses, "scope = ?")
		args = append(args, *filter.Scope)
	}
	if filter.Type != nil {
		clauses = append(clauses, "type = ?")
		args = append(args, *filter.Type)
	}
	if filter.OutputStatus != nil {
		clauses = append(clauses, "output_status = ?")
		args = append(args, *filter.OutputStatus)
	}
	if filter.ApprovalState != nil {
		clauses = append(clauses, "approval_state = ?")
		args = append(args, *filter.ApprovalState)
	}
	if filter.DryRun != nil {
		clauses = append(clauses, "dry_run = ?")
		args = append(args, *filter.DryRun)
	}
	if filter.Force != nil {
		clauses = append(clauses, "force = ?")
		args = append(args, *filter.Force)
	}
	if filter.Since != nil {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, filter.Since.UTC().Format(time.RFC3339Nano))
	}

	return clauses, args
}

func buildAuditWhere(filter AuditFilter) ([]string, []interface{}) {
	var clauses []string
	var args []interface{}

	if filter.Action != nil {
		clauses = append(clauses, "action = ?")
		args = append(args, *filter.Action)
	}
	if filter.Target != nil {
		clauses = append(clauses, "target_cluster = ?")
		args = append(args, *filter.Target)
	}
	if filter.Operator != nil {
		clauses = append(clauses, "username = ?")
		args = append(args, *filter.Operator)
	}
	if filter.Method != nil {
		clauses = append(clauses, "method = ?")
		args = append(args, *filter.Method)
	}
	if filter.ApprovalState != nil {
		clauses = append(clauses, "approval_state = ?")
		args = append(args, *filter.ApprovalState)
	}
	if filter.Since != nil {
		clauses = append(clauses, "timestamp >= ?")
		args = append(args, filter.Since.UTC().Format(time.RFC3339Nano))
	}

	return clauses, args
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

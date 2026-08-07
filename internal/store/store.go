package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/openshift-online/rosa-trusted-actions/internal/models"
)

var ErrNotFound = errors.New("not found")

type ExecutionFilter struct {
	Status        *string
	Action        *string
	Target        *string
	Operator      *string
	Scope         *string
	Type          *string
	OutputStatus  *string
	ApprovalState *string
	DryRun        *bool
	Force         *bool
	Since         *time.Time
	Limit         int
	Offset        int
}

type ExecutionListResult struct {
	Items  []models.Execution
	Total  int
	Limit  int
	Offset int
}

type AuditFilter struct {
	Action        *string
	Target        *string
	Operator      *string
	Method        *string
	ApprovalState *string
	Since         *time.Time
	Limit         int
	Offset        int
}

type AuditListResult struct {
	Items  []models.AuditEntry
	Total  int
	Limit  int
	Offset int
}

type Store interface {
	CreateExecution(ctx context.Context, exec *models.Execution) error
	GetExecution(ctx context.Context, id uuid.UUID) (*models.Execution, error)
	ListExecutions(ctx context.Context, filter ExecutionFilter) (*ExecutionListResult, error)
	UpdateExecutionStatus(ctx context.Context, id uuid.UUID, status string, completedAt *time.Time) error

	CreateAuditEntry(ctx context.Context, entry *models.AuditEntry) error
	ListAuditEntries(ctx context.Context, filter AuditFilter) (*AuditListResult, error)

	Close() error
}

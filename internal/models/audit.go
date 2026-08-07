package models

import (
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

type AuditEntry struct {
	ID            uuid.UUID `db:"id"`
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

func (a *AuditEntry) ToOpenAPI() openapi.AuditEntry {
	return openapi.AuditEntry{
		Id:            openapi_types.UUID(a.ID),
		Timestamp:     a.Timestamp,
		Method:        openapi.AuditEntryMethod(a.Method),
		Path:          a.Path,
		Username:      a.Username,
		StatusCode:    a.StatusCode,
		Action:        a.Action,
		ExecutionId:   a.ExecutionID,
		Jira:          a.Jira,
		ApprovalState: a.ApprovalState,
		TargetCluster: a.TargetCluster,
	}
}

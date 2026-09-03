package models

import (
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

type AuditEntry struct {
	ID            uuid.UUID
	Timestamp     time.Time
	Method        string
	Path          string
	Username      string
	StatusCode    int
	Action        *string
	ExecutionID   *string
	Jira          *string
	ApprovalState *string
	TargetCluster *string
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

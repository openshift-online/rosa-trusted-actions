package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

type Execution struct {
	ID               uuid.UUID
	Action           string
	Status           string
	ApprovalState    *string
	Username         *string
	TargetCluster    string
	Jira             *string
	DryRun           *bool
	Force            *bool
	Params           *json.RawMessage
	Scope            *string
	Type             *string
	Revision         *string
	ManifestWorkName *string
	RunnerSeconds    *int
	UploadSeconds    *int
	DurationSeconds  *int
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      *time.Time
}

func (e *Execution) ToOpenAPI() openapi.Execution {
	out := openapi.Execution{
		Id:               openapi_types.UUID(e.ID),
		Action:           e.Action,
		Status:           openapi.ExecutionStatus(e.Status),
		Username:         e.Username,
		TargetCluster:    e.TargetCluster,
		Jira:             e.Jira,
		DryRun:           e.DryRun,
		Force:            e.Force,
		Revision:         e.Revision,
		ManifestWorkName: e.ManifestWorkName,
		RunnerSeconds:    e.RunnerSeconds,
		UploadSeconds:    e.UploadSeconds,
		DurationSeconds:  e.DurationSeconds,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        e.UpdatedAt,
		CompletedAt:      e.CompletedAt,
	}

	if e.ApprovalState != nil {
		as := openapi.ApprovalState(*e.ApprovalState)
		out.ApprovalState = &as
	}

	if e.Scope != nil {
		s := openapi.Scope(*e.Scope)
		out.Scope = &s
	}

	if e.Type != nil {
		t := openapi.ActionType(*e.Type)
		out.Type = &t
	}

	if e.Params != nil {
		var params map[string]string
		if json.Unmarshal(*e.Params, &params) == nil {
			out.Params = &params
		}
	}

	return out
}

func ExecutionFromRequest(action string, req openapi.ExecutionRequest, username string) *Execution {
	now := time.Now().UTC()
	approvalState := string(openapi.ApprovalStateNotRequired)

	exec := &Execution{
		ID:            uuid.New(),
		Action:        action,
		Status:        string(openapi.ExecutionStatusPending),
		ApprovalState: &approvalState,
		Username:      &username,
		TargetCluster: req.TargetCluster,
		Jira:          &req.Jira,
		DryRun:        req.DryRun,
		Force:         req.Force,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if req.Params != nil {
		raw, err := json.Marshal(req.Params)
		if err == nil {
			rawMsg := json.RawMessage(raw)
			exec.Params = &rawMsg
		}
	}

	return exec
}

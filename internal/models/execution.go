package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

type Execution struct {
	ID               uuid.UUID        `db:"id"`
	Action           string           `db:"action"`
	Status           string           `db:"status"`
	ApprovalState    *string          `db:"approval_state"`
	Username         *string          `db:"username"`
	TargetCluster    string           `db:"target_cluster"`
	Jira             *string          `db:"jira"`
	DryRun           *bool            `db:"dry_run"`
	Force            *bool            `db:"force"`
	Params           *json.RawMessage `db:"params"`
	Scope            *string          `db:"scope"`
	Type             *string          `db:"type"`
	Revision         *string          `db:"revision"`
	ManifestWorkName *string          `db:"manifest_work_name"`
	OutputPath       *string          `db:"output_path"`
	OutputStatus     *string          `db:"output_status"`
	RunnerSeconds    *int             `db:"runner_seconds"`
	UploadSeconds    *int             `db:"upload_seconds"`
	DurationSeconds  *int             `db:"duration_seconds"`
	CreatedAt        time.Time        `db:"created_at"`
	UpdatedAt        time.Time        `db:"updated_at"`
	CompletedAt      *time.Time       `db:"completed_at"`
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
		OutputPath:       e.OutputPath,
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

	if e.OutputStatus != nil {
		os := openapi.OutputStatus(*e.OutputStatus)
		out.OutputStatus = &os
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

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/types"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/auth"
	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

// Catalog of available trusted actions with their authorization requirements
type catalog struct {
	actions map[string]*auth.Action
}

var _ auth.ActionCatalog = &catalog{}

func (c *catalog) GetAction(name string) (*auth.Action, bool) {
	a, ok := c.actions[name]
	return a, ok
}

func newCatalog() *catalog {
	return &catalog{actions: map[string]*auth.Action{
		"cluster-info": {
			Name:         "cluster-info",
			Description:  "Get cluster information and status",
			AllowedRoles: []string{"SREP", "ConfigurationAnomalyDetection", "ROSAAiAgent"},
		},
		"pod-restart": {
			Name:         "pod-restart",
			Description:  "Restart pods in a specific namespace",
			AllowedRoles: []string{"SREP"},
		},
	}}
}

// APIHandler implements the generated ServerInterface
type APIHandler struct {
	logger        *logrus.Logger
	ActionCatalog auth.ActionCatalog
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(logger *logrus.Logger) *APIHandler {
	return &APIHandler{
		logger:        logger,
		ActionCatalog: newCatalog(),
	}
}

// Ensure APIHandler implements ServerInterface
var _ openapi.ServerInterface = (*APIHandler)(nil)

// Catalog implements GET /
// List all available Trusted Actions
func (h *APIHandler) Catalog(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("Listing trusted actions catalog")

	// TODO: Mock response for now
	catalog := openapi.TrustedActionCatalog{
		Total: 2,
		Items: []openapi.TrustedActionSummary{
			{
				Name:        "cluster-info",
				Type:        openapi.Read,
				Scope:       openapi.KubeApi,
				Description: "Get cluster information and status",
			},
			{
				Name:        "pod-restart",
				Type:        openapi.Write,
				Scope:       openapi.KubeApi,
				Description: "Restart pods in a specific namespace",
			},
		},
	}

	render.JSON(w, r, catalog)
}

// Describe implements GET /{action}
// Get detailed description of a specific action
func (h *APIHandler) Describe(w http.ResponseWriter, r *http.Request, action string) {
	h.logger.WithField("action", action).Info("Describing trusted action")

	// TODO: Mock response for now
	description := "Target namespace"
	trustedAction := openapi.TrustedAction{
		Name:        action,
		Type:        openapi.Read,
		Scope:       openapi.KubeApi,
		Description: fmt.Sprintf("Detailed description for action: %s", action),
		Params: &[]openapi.TrustedActionParam{
			{
				Name:        "namespace",
				Description: &description,
				Required:    &[]bool{true}[0],
			},
		},
	}

	render.JSON(w, r, trustedAction)
}

// CreateExecution implements POST /{action}/run
// Execute a Trusted Action
func (h *APIHandler) CreateExecution(w http.ResponseWriter, r *http.Request, action string) {
	h.logger.WithField("action", action).Info("Creating execution for trusted action")

	// Parse request body
	var req openapi.ExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, r, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// Generate execution ID
	executionID := uuid.New()

	approval := openapi.ApprovalStateNotRequired
	identity := auth.GetCallerIdentityFromContext(r.Context())
	callerUsername := ""
	if identity != nil {
		callerUsername = identity.Username
	}

	execution := openapi.Execution{
		Id:            types.UUID(executionID),
		Action:        action,
		Status:        openapi.ExecutionStatusPending,
		ApprovalState: &approval,
		Username:      &callerUsername,
		TargetCluster: req.TargetCluster,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		DryRun:        req.DryRun,
		Force:         req.Force,
		Params:        req.Params,
	}

	// Return 202 Accepted as per API spec
	w.WriteHeader(http.StatusAccepted)
	render.JSON(w, r, execution)
}

// ListAuditEntries implements GET /audit
// List API call audit log entries
func (h *APIHandler) ListAuditEntries(w http.ResponseWriter, r *http.Request, params openapi.ListAuditEntriesParams) {
	h.logger.Info("Listing audit entries")

	auditID := uuid.New()
	executionID := "exec-123"
	jira := "OHSS-12345"
	auditIdentity := auth.GetCallerIdentityFromContext(r.Context())
	auditUsername := ""
	if auditIdentity != nil {
		auditUsername = auditIdentity.Username
	}

	audit := openapi.AuditList{
		Kind:  openapi.AuditListKindAuditList,
		Total: 1,
		Items: []openapi.AuditEntry{
			{
				Id:          types.UUID(auditID),
				Timestamp:   time.Now().Add(-10 * time.Minute),
				Method:      openapi.AuditEntryMethodPOST,
				Path:        "/api/v0/trusted-actions/cluster-info/run",
				Username:    auditUsername,
				StatusCode:  202,
				ExecutionId: &executionID,
				Jira:        &jira,
			},
		},
	}

	render.JSON(w, r, audit)
}

// ListExecutions implements GET /runs
// List executions
func (h *APIHandler) ListExecutions(w http.ResponseWriter, r *http.Request, params openapi.ListExecutionsParams) {
	h.logger.Info("Listing executions")

	h.logger.WithFields(logrus.Fields{
		"action": params.Action,
		"status": params.Status,
		"limit":  params.Limit,
	}).Debug("Execution list parameters")

	execID1 := uuid.New()
	execID2 := uuid.New()
	approval1 := openapi.ApprovalStateNotRequired
	approval2 := openapi.ApprovalStateApproved
	listIdentity := auth.GetCallerIdentityFromContext(r.Context())
	listUsername := ""
	if listIdentity != nil {
		listUsername = listIdentity.Username
	}

	executions := openapi.ExecutionList{
		HasMore: false,
		Items: []openapi.Execution{
			{
				Id:            types.UUID(execID1),
				Action:        "cluster-info",
				Status:        openapi.ExecutionStatusSucceeded,
				ApprovalState: &approval1,
				Username:      &listUsername,
				TargetCluster: "test-cluster",
				CreatedAt:     time.Now().Add(-1 * time.Hour),
				UpdatedAt:     time.Now().Add(-30 * time.Minute),
				CompletedAt:   &[]time.Time{time.Now().Add(-30 * time.Minute)}[0],
			},
			{
				Id:            types.UUID(execID2),
				Action:        "pod-restart",
				Status:        openapi.ExecutionStatusRunning,
				ApprovalState: &approval2,
				Username:      &listUsername,
				TargetCluster: "test-cluster",
				CreatedAt:     time.Now().Add(-30 * time.Minute),
				UpdatedAt:     time.Now().Add(-5 * time.Minute),
			},
		},
	}

	render.JSON(w, r, executions)
}

// GetExecution implements GET /runs/{id}
// Retrieve execution details
func (h *APIHandler) GetExecution(w http.ResponseWriter, r *http.Request, id types.UUID, params openapi.GetExecutionParams) {
	h.logger.WithField("execution_id", id).Info("Getting execution details")

	// Parse include parameters
	includeOutput := params.Include != nil && (*params.Include == openapi.Output || *params.Include == openapi.Outputlogs)
	includeLogs := params.Include != nil && (*params.Include == openapi.Logs || *params.Include == openapi.Outputlogs)

	approval := openapi.ApprovalStateNotRequired
	getIdentity := auth.GetCallerIdentityFromContext(r.Context())
	getUsername := ""
	if getIdentity != nil {
		getUsername = getIdentity.Username
	}
	outputPath := "s3://trusted-actions-bucket/outputs/exec-123/output.json"
	outputStatus := openapi.OutputStatusUploaded

	execution := openapi.Execution{
		Id:            id,
		Action:        "cluster-info",
		Status:        openapi.ExecutionStatusSucceeded,
		ApprovalState: &approval,
		Username:      &getUsername,
		TargetCluster: "test-cluster",
		CreatedAt:     time.Now().Add(-1 * time.Hour),
		UpdatedAt:     time.Now().Add(-30 * time.Minute),
		CompletedAt:   &[]time.Time{time.Now().Add(-30 * time.Minute)}[0],
		RunnerSeconds: &[]int{45}[0],
		UploadSeconds: &[]int{2}[0],
		OutputPath:    &outputPath,
		OutputStatus:  &outputStatus,
	}

	// TODO Add output if requested
	if includeOutput {
		var outputData interface{} = map[string]interface{}{
			"nodes":  3,
			"pods":   45,
			"status": "healthy",
		}
		execution.Output = &outputData
	}

	// Add logs if requested
	if includeLogs {
		logs := "Execution started...\nConnecting to cluster...\nExecution completed successfully."
		execution.Logs = &logs
	}

	render.JSON(w, r, execution)
}

// Helper functions

func (h *APIHandler) respondError(w http.ResponseWriter, r *http.Request, status int, message string, err error) {
	h.logger.WithError(err).Error(message)

	errorResp := openapi.Error{
		Kind:   openapi.ErrorKindError,
		Code:   fmt.Sprintf("HTTP_%d", status),
		Reason: message,
	}

	w.WriteHeader(status)
	render.JSON(w, r, errorResp)
}

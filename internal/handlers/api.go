package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/render"
	"github.com/oapi-codegen/runtime/types"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/auth"
	"github.com/openshift-online/rosa-trusted-actions/internal/models"
	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
	"github.com/openshift-online/rosa-trusted-actions/internal/store"
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
	store         store.Store
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(logger *logrus.Logger, s store.Store) *APIHandler {
	return &APIHandler{
		logger:        logger,
		ActionCatalog: newCatalog(),
		store:         s,
	}
}

// Ensure APIHandler implements ServerInterface
var _ openapi.ServerInterface = (*APIHandler)(nil)

// Catalog implements GET /
// List all available Trusted Actions
func (h *APIHandler) Catalog(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("Listing trusted actions catalog")

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

	var req openapi.ExecutionRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		h.respondError(w, r, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	identity := auth.GetCallerIdentityFromContext(r.Context())
	username := ""
	if identity != nil {
		username = identity.Username
	}

	exec := models.ExecutionFromRequest(action, req, username)

	if err := h.store.CreateExecution(r.Context(), exec); err != nil {
		h.respondError(w, r, http.StatusInternalServerError, "Failed to create execution", err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	render.JSON(w, r, exec.ToOpenAPI())
}

// ListAuditEntries implements GET /audit
// List API call audit log entries
func (h *APIHandler) ListAuditEntries(w http.ResponseWriter, r *http.Request, params openapi.ListAuditEntriesParams) {
	h.logger.Info("Listing audit entries")

	filter := store.AuditFilter{
		Action: params.Action,
		Target: params.Target,
	}

	if params.Operator != nil {
		filter.Operator = params.Operator
	}
	if params.Method != nil {
		m := string(*params.Method)
		filter.Method = &m
	}
	if params.ApprovalState != nil {
		a := string(*params.ApprovalState)
		filter.ApprovalState = &a
	}
	if params.Limit != nil {
		filter.Limit = *params.Limit
	}
	if params.Since != nil {
		t, err := parseSince(*params.Since)
		if err != nil {
			h.respondError(w, r, http.StatusBadRequest, "Invalid since parameter", err)
			return
		}
		filter.Since = t
	}

	result, err := h.store.ListAuditEntries(r.Context(), filter)
	if err != nil {
		h.respondError(w, r, http.StatusInternalServerError, "Failed to list audit entries", err)
		return
	}

	items := make([]openapi.AuditEntry, 0, len(result.Items))
	for _, entry := range result.Items {
		items = append(items, entry.ToOpenAPI())
	}

	render.JSON(w, r, openapi.AuditList{
		Kind:  openapi.AuditListKindAuditList,
		Total: result.Total,
		Items: items,
	})
}

// ListExecutions implements GET /runs
// List executions
func (h *APIHandler) ListExecutions(w http.ResponseWriter, r *http.Request, params openapi.ListExecutionsParams) {
	h.logger.Info("Listing executions")

	filter := store.ExecutionFilter{
		Action: params.Action,
		Target: params.Target,
	}

	if params.Operator != nil {
		filter.Operator = params.Operator
	}
	if params.Status != nil {
		s := string(*params.Status)
		filter.Status = &s
	}
	if params.Scope != nil {
		s := string(*params.Scope)
		filter.Scope = &s
	}
	if params.Type != nil {
		t := string(*params.Type)
		filter.Type = &t
	}
	if params.OutputStatus != nil {
		o := string(*params.OutputStatus)
		filter.OutputStatus = &o
	}
	if params.ApprovalState != nil {
		a := string(*params.ApprovalState)
		filter.ApprovalState = &a
	}
	if params.DryRun != nil {
		b := *params.DryRun == openapi.ListExecutionsParamsDryRunTrue
		filter.DryRun = &b
	}
	if params.Force != nil {
		b := *params.Force == openapi.ListExecutionsParamsForceTrue
		filter.Force = &b
	}
	if params.Limit != nil {
		filter.Limit = *params.Limit
	}
	if params.Since != nil {
		t, err := parseSince(*params.Since)
		if err != nil {
			h.respondError(w, r, http.StatusBadRequest, "Invalid since parameter", err)
			return
		}
		filter.Since = t
	}

	result, err := h.store.ListExecutions(r.Context(), filter)
	if err != nil {
		h.respondError(w, r, http.StatusInternalServerError, "Failed to list executions", err)
		return
	}

	items := make([]openapi.Execution, 0, len(result.Items))
	for _, exec := range result.Items {
		items = append(items, exec.ToOpenAPI())
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	render.JSON(w, r, openapi.ExecutionList{
		Items:   items,
		Total:   result.Total,
		Page:    1,
		Limit:   limit,
		HasMore: result.Total > len(items),
	})
}

// GetExecution implements GET /runs/{id}
// Retrieve execution details
func (h *APIHandler) GetExecution(w http.ResponseWriter, r *http.Request, id types.UUID, params openapi.GetExecutionParams) {
	h.logger.WithField("execution_id", id).Info("Getting execution details")

	exec, err := h.store.GetExecution(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.respondError(w, r, http.StatusNotFound, "Execution not found", err)
			return
		}
		h.respondError(w, r, http.StatusInternalServerError, "Failed to get execution", err)
		return
	}

	result := exec.ToOpenAPI()

	includeOutput := params.Include != nil && (*params.Include == openapi.Output || *params.Include == openapi.Outputlogs)
	includeLogs := params.Include != nil && (*params.Include == openapi.Logs || *params.Include == openapi.Outputlogs)

	// Logs and Output are fetched from S3 at request time, not from the database.
	// Placeholder until S3 retrieval is implemented.
	if includeOutput {
		h.logger.WithField("execution_id", id).Debug("Output retrieval not yet implemented")
	}
	if includeLogs {
		h.logger.WithField("execution_id", id).Debug("Log retrieval not yet implemented")
	}

	render.JSON(w, r, result)
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

func parseSince(since string) (*time.Time, error) {
	if t, err := time.Parse(time.RFC3339, since); err == nil {
		return &t, nil
	}

	since = strings.TrimSpace(since)
	if len(since) < 2 {
		return nil, fmt.Errorf("invalid duration: %s", since)
	}

	unit := since[len(since)-1]
	numStr := since[:len(since)-1]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return nil, fmt.Errorf("invalid duration: %s", since)
	}

	var d time.Duration
	switch unit {
	case 's':
		d = time.Duration(num) * time.Second
	case 'm':
		d = time.Duration(num) * time.Minute
	case 'h':
		d = time.Duration(num) * time.Hour
	case 'd':
		d = time.Duration(num) * 24 * time.Hour
	default:
		return nil, fmt.Errorf("unknown duration unit: %c", unit)
	}

	t := time.Now().UTC().Add(-d)
	return &t, nil
}

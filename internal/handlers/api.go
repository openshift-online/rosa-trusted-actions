package handlers

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/render"
	"github.com/oapi-codegen/runtime/types"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/auth"
	"github.com/openshift-online/rosa-trusted-actions/internal/catalog"
	"github.com/openshift-online/rosa-trusted-actions/internal/middleware"
	"github.com/openshift-online/rosa-trusted-actions/internal/models"
	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
	"github.com/openshift-online/rosa-trusted-actions/internal/store"
)

// ExecutionNotifier is notified when a new execution is ready to be
// dequeued, so a worker can wake immediately instead of waiting for its next
// poll. Implemented by worker.Pool; declared here to keep handlers decoupled
// from the internal/worker package.
type ExecutionNotifier interface {
	Notify()
}

// APIHandler implements the generated ServerInterface
type APIHandler struct {
	logger        *logrus.Logger
	ActionCatalog auth.ActionCatalog
	catalog       *catalog.Catalog
	store         store.Store
	notifier      ExecutionNotifier
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(logger *logrus.Logger, c *catalog.Catalog, s store.Store, notifier ExecutionNotifier) *APIHandler {
	return &APIHandler{
		logger:        logger,
		ActionCatalog: c,
		catalog:       c,
		store:         s,
		notifier:      notifier,
	}
}

// Ensure APIHandler implements ServerInterface
var _ openapi.ServerInterface = (*APIHandler)(nil)

// Catalog implements GET /
// List all available Trusted Actions
func (h *APIHandler) Catalog(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("Listing trusted actions catalog")

	all := h.catalog.All()
	items := make([]openapi.TrustedActionSummary, 0, len(all))
	for _, a := range all {
		items = append(items, a.ToOpenAPISummary())
	}

	render.JSON(w, r, openapi.TrustedActionCatalog{
		Total: len(all),
		Items: items,
	})
}

// Describe implements GET /{action}
// Get detailed description of a specific action
func (h *APIHandler) Describe(w http.ResponseWriter, r *http.Request, action string) {
	h.logger.WithField("action", action).Info("Describing trusted action")

	def, ok := h.catalog.Get(action)
	if !ok {
		h.respondError(w, r, http.StatusNotFound, "Unknown action", fmt.Errorf("action %q not found in catalog", action))
		return
	}

	render.JSON(w, r, def.ToOpenAPIDetail())
}

// CreateExecution implements POST /{action}/run
// Execute a Trusted Action against a target cluster.
// The request is persisted as a pending execution and the response returns
// immediately; a background worker pool (internal/worker) dequeues and runs
// it asynchronously. Poll GET /runs/{id} for status.
func (h *APIHandler) CreateExecution(w http.ResponseWriter, r *http.Request, action string) {
	h.logger.WithField("action", action).Info("Creating execution for trusted action")

	if _, ok := h.catalog.Get(action); !ok {
		h.respondError(w, r, http.StatusNotFound, "Unknown action", fmt.Errorf("action %q not found in catalog", action))
		return
	}

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

	h.notifier.Notify()

	if ac := middleware.GetAuditContext(r.Context()); ac != nil {
		ac.ExecutionID = exec.ID.String()
		ac.TargetCluster = exec.TargetCluster
		if exec.Jira != nil {
			ac.Jira = *exec.Jira
		}
		if exec.ApprovalState != nil {
			ac.ApprovalState = *exec.ApprovalState
		}
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

	// Resolve effective limit first (store default: 50)
	effectiveLimit := 50
	if params.Limit != nil {
		effectiveLimit = *params.Limit
	}
	filter.Limit = effectiveLimit

	// Validate and compute offset
	if params.Page != nil {
		page := *params.Page
		if page < 1 {
			h.respondError(w, r, http.StatusBadRequest, "Invalid page parameter", fmt.Errorf("page must be >= 1, got %d", page))
			return
		}
		if page > 1 {
			// Check for overflow: (page-1) * effectiveLimit
			if page-1 > (1<<31-1)/effectiveLimit {
				h.respondError(w, r, http.StatusBadRequest, "Invalid page parameter", fmt.Errorf("page %d too large", page))
				return
			}
			filter.Offset = (page - 1) * effectiveLimit
		}
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

	page := result.Offset/result.Limit + 1
	render.JSON(w, r, openapi.AuditList{
		Kind:    openapi.AuditListKindAuditList,
		Total:   result.Total,
		Page:    page,
		Limit:   result.Limit,
		HasMore: result.Offset+len(items) < result.Total,
		Items:   items,
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

	// Resolve effective limit first (store default: 20)
	effectiveLimit := 20
	if params.Limit != nil {
		effectiveLimit = *params.Limit
	}
	filter.Limit = effectiveLimit

	// Validate and compute offset
	if params.Page != nil {
		page := *params.Page
		if page < 1 {
			h.respondError(w, r, http.StatusBadRequest, "Invalid page parameter", fmt.Errorf("page must be >= 1, got %d", page))
			return
		}
		if page > 1 {
			// Check for overflow: (page-1) * effectiveLimit
			if page-1 > (1<<31-1)/effectiveLimit {
				h.respondError(w, r, http.StatusBadRequest, "Invalid page parameter", fmt.Errorf("page %d too large", page))
				return
			}
			filter.Offset = (page - 1) * effectiveLimit
		}
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

	page := result.Offset/result.Limit + 1
	render.JSON(w, r, openapi.ExecutionList{
		Items:   items,
		Total:   result.Total,
		Page:    page,
		Limit:   result.Limit,
		HasMore: result.Offset+len(items) < result.Total,
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
	if num <= 0 {
		return nil, fmt.Errorf("duration must be positive: %s", since)
	}

	var multiplier int64
	switch unit {
	case 's':
		multiplier = int64(time.Second)
	case 'm':
		multiplier = int64(time.Minute)
	case 'h':
		multiplier = int64(time.Hour)
	case 'd':
		multiplier = 24 * int64(time.Hour)
	default:
		return nil, fmt.Errorf("unknown duration unit: %c", unit)
	}

	const maxDurationNs = int64(math.MaxInt64)
	if int64(num) > maxDurationNs/multiplier {
		return nil, fmt.Errorf("duration too large: %s", since)
	}
	d := time.Duration(int64(num) * multiplier)

	t := time.Now().UTC().Add(-d)
	return &t, nil
}

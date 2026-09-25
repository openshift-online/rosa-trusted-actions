package handlers

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/render"
	"github.com/oapi-codegen/runtime/types"
	acctrspv1 "github.com/openshift-online/ocm-sdk-go/accesstransparency/v1"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/auth"
	"github.com/openshift-online/rosa-trusted-actions/internal/catalog"
	"github.com/openshift-online/rosa-trusted-actions/internal/middleware"
	"github.com/openshift-online/rosa-trusted-actions/internal/models"
	"github.com/openshift-online/rosa-trusted-actions/internal/ocm"
	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
	"github.com/openshift-online/rosa-trusted-actions/internal/store"
	"github.com/openshift-online/rosa-trusted-actions/internal/worker"
)

var clusterIDRegexp = regexp.MustCompile("^[a-z0-9]+$")

// ExecutionNotifier is notified when a new execution is ready to be
// dequeued, so a worker can wake immediately instead of waiting for its next
// poll. Implemented by worker.Pool; declared here to keep handlers decoupled
// from the internal/worker package.
type ExecutionNotifier interface {
	Notify()
}

// APIHandler implements the generated ServerInterface
type APIHandler struct {
	logger           *logrus.Logger
	ActionCatalog    auth.ActionCatalog
	catalog          *catalog.Catalog
	accessProtection ocm.AccessProtection
	runner           worker.Runner
	store            store.Store
	notifier         ExecutionNotifier
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(logger *logrus.Logger, c *catalog.Catalog, a ocm.AccessProtection, r worker.Runner, s store.Store, notifier ExecutionNotifier) *APIHandler {
	return &APIHandler{
		logger:           logger,
		ActionCatalog:    c,
		catalog:          c,
		accessProtection: a,
		runner:           r,
		store:            s,
		notifier:         notifier,
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

type runnableReport struct {
	isRunnable        bool
	notRunnableReason string
}

func (h *APIHandler) checkExecutionIsRunnable(ctx context.Context, exec *models.Execution) (runnableReport, error) {
	if exec.ApprovalState == nil {
		switch openapi.ApprovalState(*exec.ApprovalState) {
		case openapi.ApprovalStateNotRequired:
		case openapi.ApprovalStateApproved:
		default:
			return runnableReport{
				isRunnable:        false,
				notRunnableReason: "execution has not yet been approved",
			}, nil
		}
	}

	accessProtectionEnabled, err := h.accessProtection.IsAccessProtectionEnabled(ctx, exec.TargetCluster)
	if err != nil {
		return runnableReport{
			isRunnable:        false,
			notRunnableReason: "error",
		}, err
	}
	if !accessProtectionEnabled {
		return runnableReport{
			isRunnable:        true,
			notRunnableReason: "",
		}, nil
	}

	accessRequest, err := h.accessProtection.GetClusterActiveAccessRequest(ctx, exec.TargetCluster)
	if err != nil {
		return runnableReport{
			isRunnable:        false,
			notRunnableReason: "error",
		}, err
	}
	if accessRequest == nil {
		return runnableReport{
			isRunnable:        false,
			notRunnableReason: fmt.Sprintf("cluster access is protected but there is no active access request - run `ocm-backplane accessrequest create` to create one"),
		}, nil
	}

	accessRequestStatus := accessRequest.Status()

	if accessRequestStatus == nil || accessRequestStatus.State() != acctrspv1.AccessRequestStateApproved {
		return runnableReport{
			isRunnable:        false,
			notRunnableReason: fmt.Sprintf("access request %s is not yet approved", accessRequest.HREF()),
		}, nil
	}

	return runnableReport{
		isRunnable:        true,
		notRunnableReason: "",
	}, nil
}

// CreateExecution implements POST /{action}/run
// Execute a Trusted Action against a target cluster.
// The request is persisted as a pending execution and the response returns
// immediately; a background worker pool (internal/worker) dequeues and runs
// it asynchronously. Poll GET /runs/{id} for status.
func (h *APIHandler) CreateExecution(w http.ResponseWriter, r *http.Request, action string, params openapi.CreateExecutionParams) {
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

	if !clusterIDRegexp.MatchString(req.TargetCluster) {
		h.respondError(w, r, http.StatusBadRequest, "Invalid cluster ID", fmt.Errorf("'%s' is not a valid cluster ID", req.TargetCluster))
		return
	}

	identity := auth.GetCallerIdentityFromContext(r.Context())
	username := ""
	if identity != nil {
		username = identity.Username
	}

	exec := models.ExecutionFromRequest(action, req, username)
	var output *models.ExecutionOutput

	if params.ReplyMode == nil || *params.ReplyMode != openapi.Async {
		// We maybe have to run synchronously
		report, err := h.checkExecutionIsRunnable(r.Context(), exec)
		if err != nil {
			h.respondError(w, r, http.StatusInternalServerError, "Failed to check execution runnable status", err)
			return
		}

		if report.isRunnable {
			result := h.runner.Run(r.Context(), exec)
			if result.Reason != "" {
				h.respondError(w, r, http.StatusInternalServerError, "Execution failed", errors.New(result.Reason))
				return
			}

			exec.Status = result.Status // To be sure the execution does not get consumed by the workers when written in DB
			output = models.OutputFromActionResult(result.Output)
		} else {
			if params.ReplyMode != nil && *params.ReplyMode == openapi.Sync {
				h.respondError(w, r, http.StatusBadRequest, "Execution is not yet runnable", errors.New(report.notRunnableReason))
				return
			} else {
				h.logger.WithField("reason", report.notRunnableReason).Info("Execution is not runnable yet, gonna be processed asynchronously")
			}
		}
	}

	if err := h.store.CreateExecution(r.Context(), exec); err != nil {
		h.respondError(w, r, http.StatusInternalServerError, "Failed to create execution", err)
		return
	}

	if output != nil {
		completedAt := time.Now().UTC()
		// Detached from ctx's cancellation: if ctx is cancelled (e.g. shutdown)
		// right as Run finishes, we've already got execution output and want to persist it.
		updateCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()

		if err := h.store.UpdateExecutionWithResult(updateCtx, exec.ID, exec.Status, &completedAt, output); err != nil {
			h.respondError(w, r, http.StatusInternalServerError, "Failed to update execution with its output", err)
			return
		}
	} else {
		h.notifier.Notify()
	}

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

	result := openapi.ExecutionWithOutput{
		Execution: exec.ToOpenAPI(),
	}
	baseHref := path.Join(path.Dir(path.Dir(r.URL.Path)), "runs", exec.ID.String())

	result.Execution.UnderscoreLinks = &openapi.ExecutionLinks{
		Self: openapi.HALLink{
			Href:   baseHref,
			Method: "GET",
		},
	}

	if output != nil {
		result.Execution.UnderscoreLinks.Output = &openapi.HALLink{
			Href:   path.Join(baseHref, "output"),
			Method: "GET",
		}

		resultOutput := output.ToOpenAPI()
		result.Output = &resultOutput
		result.Output.UnderscoreLinks = &openapi.ExecutionOutputLinks{
			Self: openapi.HALLink{
				Href:   path.Join(baseHref, "output"),
				Method: "GET",
			},
			Execution: openapi.HALLink{
				Href:   baseHref,
				Method: "GET",
			},
		}
	}

	w.WriteHeader(http.StatusAccepted)
	render.JSON(w, r, result)
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
func (h *APIHandler) GetExecution(w http.ResponseWriter, r *http.Request, id types.UUID) {
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

	result.UnderscoreLinks = &openapi.ExecutionLinks{
		Self: openapi.HALLink{
			Href:   r.URL.Path,
			Method: "GET",
		},
	}

	{
		_, err := h.store.GetExecutionOutput(r.Context(), id)
		if err == nil {
			result.UnderscoreLinks.Output = &openapi.HALLink{
				Href:   path.Join(r.URL.Path, "output"),
				Method: "GET",
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			h.logger.WithError(err).Warn("checking output existence")
		}
	}

	render.JSON(w, r, result)
}

// GetExecutionOutput implements GET /runs/{exec_id}/output
// Retrieve execution output
func (h *APIHandler) GetExecutionOutput(w http.ResponseWriter, r *http.Request, execId types.UUID) {
	h.logger.WithField("execution_id", execId).Info("Getting execution details")

	output, err := h.store.GetExecutionOutput(r.Context(), execId)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.respondError(w, r, http.StatusNotFound, "Output not found", err)
			return
		}
		h.respondError(w, r, http.StatusInternalServerError, "Failed to get output", err)
		return
	}

	result := output.ToOpenAPI()

	result.UnderscoreLinks = &openapi.ExecutionOutputLinks{
		Self: openapi.HALLink{
			Href:   r.URL.Path,
			Method: "GET",
		},
		Execution: openapi.HALLink{
			Href:   path.Dir(r.URL.Path),
			Method: "GET",
		},
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

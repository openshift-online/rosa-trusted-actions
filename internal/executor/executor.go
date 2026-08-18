package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/audit"
	"github.com/openshift-online/rosa-trusted-actions/internal/authorization"
	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
)

type Request struct {
	CallerID       string
	ClusterID      string
	ClusterVersion string
	Action         actions.Action
	Target         actions.ResourceTarget
	Params         map[string]string
}

type Result struct {
	Allowed bool
	Reason  string
	Output  *actions.ActionResult
	Error   error
}

type Executor struct {
	logger     *logrus.Logger
	authorizer authorization.Authorizer
	auditor    audit.Logger
	backplane  backplane.ClientProvider
}

func New(
	logger *logrus.Logger,
	authorizer authorization.Authorizer,
	auditor audit.Logger,
	bp backplane.ClientProvider,
) *Executor {
	return &Executor{
		logger:     logger,
		authorizer: authorizer,
		auditor:    auditor,
		backplane:  bp,
	}
}

func (e *Executor) Execute(ctx context.Context, req Request) (result *Result) {
	rec := audit.Record{
		Timestamp: time.Now(),
		CallerID:  req.CallerID,
		Action:    req.Action.Name(),
		Target:    req.Target,
		ClusterID: req.ClusterID,
	}
	defer func() { e.auditor.Log(rec) }()

	authResult := e.authorizer.Authorize(authorization.Request{
		Namespace:     req.Target.Namespace,
		ResourceType:  req.Target.Resource,
		ResourceName:  req.Target.Name,
		ClusterScoped: req.Target.ClusterScoped,
	})

	if !authResult.Allowed {
		rec.Decision = audit.DecisionDenied
		rec.DenyReason = authResult.Reason
		rec.Outcome = audit.OutcomeSkipped
		return &Result{
			Allowed: false,
			Reason:  authResult.Reason,
		}
	}

	rec.Decision = audit.DecisionAllowed

	// Each primitive action gets its own backplane session scoped to exactly the
	// RBAC it needs (least-privilege). Composite actions could merge RBAC rules
	// from all sub-actions and call GetClient once to reuse a single session.
	// (possible design decision for later)
	rbacRules := req.Action.RequiredRBAC(req.Target)
	client, err := e.backplane.GetClient(ctx, req.ClusterID, rbacRules)
	if err != nil {
		rec.Outcome = audit.OutcomeFailure
		rec.Error = err.Error()
		return &Result{
			Allowed: true,
			Reason:  authResult.Reason,
			Error:   fmt.Errorf("failed to get cluster client: %w", err),
		}
	}

	clients := actions.Clients{Dynamic: client}

	if req.Action.UsesPodExec() {
		podExec, podErr := e.backplane.GetPodExecutor(ctx, req.ClusterID, rbacRules)
		if podErr != nil {
			rec.Outcome = audit.OutcomeFailure
			rec.Error = podErr.Error()
			return &Result{
				Allowed: true,
				Reason:  authResult.Reason,
				Error:   fmt.Errorf("failed to get pod executor: %w", podErr),
			}
		}
		clients.PodExecutor = podExec
	}

	actionReq := actions.ActionRequest{
		Target:         req.Target,
		ClusterVersion: req.ClusterVersion,
		Params:         req.Params,
	}
	output, err := req.Action.Execute(ctx, clients, actionReq)
	if err != nil {
		rec.Outcome = audit.OutcomeFailure
		rec.Error = err.Error()
		return &Result{
			Allowed: true,
			Reason:  authResult.Reason,
			Error:   fmt.Errorf("action execution failed: %w", err),
		}
	}

	rec.Outcome = audit.OutcomeSuccess
	return &Result{
		Allowed: true,
		Reason:  authResult.Reason,
		Output:  output,
	}
}

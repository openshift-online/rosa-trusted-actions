package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/audit"
	"github.com/openshift-online/rosa-trusted-actions/internal/authorization"
)

type Request struct {
	CallerID       string
	ClusterID      string
	ClusterVersion string
	Action         actions.Action
	// Target is what the action acts upon. When nil, the resolved Scope
	// derives it from Params.
	Target actions.Target
	Params map[string]string
}

type Result struct {
	Allowed bool
	Reason  string
	Output  *actions.ActionResult
	Error   error
}

// Scope is one execution domain — the Kubernetes API of a managed cluster
// today, a cloud provider account later. A scope owns everything specific to
// its domain: which actions belong to it, how their targets are shaped, how a
// request is authorized, and how the clients an action needs are provisioned.
// Executor itself stays domain-agnostic, so a new scope cannot be added
// without deciding how it authorizes.
type Scope interface {
	// Supports reports whether this scope can run the given action.
	Supports(action actions.Action) bool

	// DeriveTarget builds this scope's target from the request's params. It is
	// used for requests that carry no explicit target. It takes a context and
	// can fail because filling in a default may require a lookup — the AWS
	// scope resolves the cluster's own region, which it has to ask OCM for.
	DeriveTarget(ctx context.Context, req Request) (actions.Target, error)

	// Authorize decides whether the request is permitted.
	Authorize(req Request) authorization.Result

	// Run provisions the clients the action needs and executes it. Failures
	// are reported as *Failure so that the caller-facing message and the
	// audited cause stay distinct.
	Run(ctx context.Context, req Request) (*actions.ActionResult, error)
}

// Failure is returned by a Scope when a stage of the execution fails. Message
// frames the failure for the caller, while Cause is the underlying error and
// is what the audit trail records.
type Failure struct {
	Message string
	Cause   error
}

// Error tolerates a nil Cause. Every Failure raised today wraps one, but the
// field is optional as far as the type is concerned, and the cost of being
// wrong about that is a panic on the request path rather than an error.
func (f *Failure) Error() string {
	if f.Cause == nil {
		return f.Message
	}
	return f.Message + ": " + f.Cause.Error()
}

func (f *Failure) Unwrap() error { return f.Cause }

type Executor struct {
	logger  *logrus.Logger
	auditor audit.Logger
	scopes  []Scope
}

func New(logger *logrus.Logger, auditor audit.Logger, scopes ...Scope) *Executor {
	return &Executor{
		logger:  logger,
		auditor: auditor,
		scopes:  scopes,
	}
}

func (e *Executor) Execute(ctx context.Context, req Request) (result *Result) {
	rec := audit.Record{
		Timestamp: time.Now(),
		CallerID:  req.CallerID,
		Action:    req.Action.Name(),
		ClusterID: req.ClusterID,
	}
	defer func() { e.auditor.Log(rec) }()

	scope := e.scopeFor(req.Action)
	if scope == nil {
		reason := fmt.Sprintf("no execution scope configured for action %q", req.Action.Name())
		rec.Decision = audit.DecisionDenied
		rec.DenyReason = reason
		rec.Outcome = audit.OutcomeSkipped
		return &Result{
			Allowed: false,
			Reason:  reason,
		}
	}

	if req.Target == nil {
		target, err := scope.DeriveTarget(ctx, req)
		if err != nil {
			// Not a denial: authorization never got to run, because there was
			// no target to judge. Reported as a failure so the caller is not
			// told they were refused something they were never refused.
			rec.Outcome = audit.OutcomeFailure
			rec.Error = auditedCause(err).Error()
			return &Result{Error: err}
		}
		req.Target = target
	}
	rec.Target = req.Target

	authResult := scope.Authorize(req)

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

	output, err := scope.Run(ctx, req)
	if err != nil {
		rec.Outcome = audit.OutcomeFailure
		rec.Error = auditedCause(err).Error()
		return &Result{
			Allowed: true,
			Reason:  authResult.Reason,
			Error:   err,
		}
	}

	rec.Outcome = audit.OutcomeSuccess
	return &Result{
		Allowed: true,
		Reason:  authResult.Reason,
		Output:  output,
	}
}

func (e *Executor) scopeFor(action actions.Action) Scope {
	for _, scope := range e.scopes {
		if scope.Supports(action) {
			return scope
		}
	}
	return nil
}

// auditedCause strips the caller-facing framing a Scope added, so the audit
// trail records the underlying failure rather than the pipeline's wording.
func auditedCause(err error) error {
	var failure *Failure
	if errors.As(err, &failure) && failure.Cause != nil {
		return failure.Cause
	}
	return err
}

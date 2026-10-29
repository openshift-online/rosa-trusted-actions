package executor

import (
	"context"
	"fmt"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/authorization"
	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

// AWSScope runs actions against the AWS API, reaching it through credentials
// the provider resolves for the target cluster's account.
type AWSScope struct {
	authorizer authorization.AWSAuthorizer
	aws        awsapi.ClientProvider
}

var _ Scope = (*AWSScope)(nil)

func NewAWSScope(authorizer authorization.AWSAuthorizer, provider awsapi.ClientProvider) *AWSScope {
	return &AWSScope{
		authorizer: authorizer,
		aws:        provider,
	}
}

func (s *AWSScope) Supports(action actions.Action) bool {
	_, ok := action.(actions.AWSAction)
	return ok
}

func (s *AWSScope) DeriveTarget(ctx context.Context, req Request) (actions.Target, error) {
	params := req.Params

	// The region is always the target cluster's own and is never taken from
	// the caller: a cluster's AWS resources live where the cluster does, so
	// naming any other region could only ever read an account the credentials
	// do not reach or an empty corner of the one they do.
	//
	// This is a lookup, not a constant — which region that is depends on which
	// cluster was named. Resolved here rather than inside the provider so the
	// region the request is authorized and audited against is the one it runs
	// in.
	region, err := s.aws.DefaultRegion(ctx, req.ClusterID)
	if err != nil {
		return nil, &Failure{Message: "failed to resolve the cluster's AWS region", Cause: err}
	}

	target := actions.AWSTarget{
		Region:   region,
		Resource: params["resource"],
		ID:       params["id"],
	}

	// A write action is more constrained than a read. It says itself what kind
	// of object it acts on, and it needs to know which cluster owns the one it
	// was pointed at.
	write, ok := req.Action.(actions.AWSWriteAction)
	if !ok {
		return target, nil
	}

	// Refused rather than overridden, for the same reason aws-get refuses a
	// param it does not understand: a caller who sent a resource has an
	// expectation about what is being changed, and quietly acting on something
	// else is the worst possible way to correct them.
	//
	// A plain error, not a Failure: nothing failed. The request was understood
	// and refused, the same way aws-get refuses a param its resource does not
	// accept.
	if target.Resource != "" && target.Resource != write.Resource() {
		return nil, fmt.Errorf("action %q acts on %s, not %q", req.Action.Name(), write.Resource(), target.Resource)
	}
	target.Resource = write.Resource()

	// Resolved from OCM alongside the region, and for the same reason: it is
	// what the action checks the instance's ownership tag against, so it must
	// come from the cluster record rather than from the request. A failure here
	// stops the execution — an ownership check with nothing to compare against
	// would not be one.
	infraID, err := s.aws.ClusterInfraID(ctx, req.ClusterID)
	if err != nil {
		return nil, &Failure{Message: "failed to resolve the cluster's infrastructure ID", Cause: err}
	}
	target.InfraID = infraID

	return target, nil
}

func (s *AWSScope) Authorize(req Request) authorization.Result {
	target, ok := req.Target.(actions.AWSTarget)
	if !ok {
		return authorization.Result{
			Allowed: false,
			Reason:  fmt.Sprintf("target %T is not an AWS target", req.Target),
		}
	}

	// Taken from the action, not from the request: the operation decides which
	// allowlist a request is judged against, so letting a caller influence it
	// would let them pick their own gate. An action that is not a write leaves
	// it empty and is judged as a read.
	operation := ""
	if write, ok := req.Action.(actions.AWSWriteAction); ok {
		operation = write.Operation()
	}

	return s.authorizer.Authorize(authorization.AWSRequest{
		Region:    target.Region,
		Resource:  target.Resource,
		Operation: operation,
	})
}

func (s *AWSScope) Run(ctx context.Context, req Request) (*actions.ActionResult, error) {
	action, ok := req.Action.(actions.AWSAction)
	if !ok {
		return nil, fmt.Errorf("action %q is not an AWS action", req.Action.Name())
	}
	target, ok := req.Target.(actions.AWSTarget)
	if !ok {
		return nil, fmt.Errorf("target %T is not an AWS target", req.Target)
	}

	// Only the services the action declared are built, so an execution never
	// holds a client it has no reason to use.
	services := action.RequiredServices(target)
	clients, err := s.aws.GetClients(ctx, req.ClusterID, target.Region, services)
	if err != nil {
		return nil, &Failure{Message: "failed to get AWS clients", Cause: err}
	}

	output, err := action.Execute(ctx, clients, actions.AWSActionRequest{
		Target: target,
		Params: req.Params,
	})
	if err != nil {
		return nil, &Failure{Message: "action execution failed", Cause: err}
	}

	return output, nil
}

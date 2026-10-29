package executor

import (
	"context"
	"fmt"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/authorization"
	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
)

// KubeScope runs actions against the Kubernetes API of a managed cluster,
// reaching it through a backplane session scoped to the action's RBAC.
type KubeScope struct {
	authorizer authorization.Authorizer
	backplane  backplane.ClientProvider
}

var _ Scope = (*KubeScope)(nil)

func NewKubeScope(authorizer authorization.Authorizer, bp backplane.ClientProvider) *KubeScope {
	return &KubeScope{
		authorizer: authorizer,
		backplane:  bp,
	}
}

func (s *KubeScope) Supports(action actions.Action) bool {
	_, ok := action.(actions.KubeAction)
	return ok
}

// DeriveTarget builds a Kubernetes target purely from the params, so it never
// fails and needs neither the context nor the rest of the request.
func (s *KubeScope) DeriveTarget(_ context.Context, req Request) (actions.Target, error) {
	params := req.Params
	return actions.ResourceTarget{
		Group:     params["group"],
		Version:   params["version"],
		Resource:  params["resource"],
		Name:      params["name"],
		Namespace: params["namespace"],
		// ClusterScoped is intentionally never derived from a missing
		// namespace param: whether the target resource type is actually
		// cluster-scoped is not something a caller-supplied (or absent)
		// value can be trusted to declare. Doing so previously let a
		// caller bypass the namespace allowlist entirely, since
		// authorization.Authorize's "namespace required" check never
		// fires once ClusterScoped is true. Until cluster-scoped targets
		// can be derived from something the caller doesn't control (e.g.
		// action/resource metadata), treat every derived target as
		// namespaced.
	}, nil
}

func (s *KubeScope) Authorize(req Request) authorization.Result {
	target, ok := req.Target.(actions.ResourceTarget)
	if !ok {
		return authorization.Result{
			Allowed: false,
			Reason:  fmt.Sprintf("target %T is not a Kubernetes resource target", req.Target),
		}
	}

	return s.authorizer.Authorize(authorization.Request{
		Namespace:     target.Namespace,
		ResourceType:  target.Resource,
		ResourceName:  target.Name,
		ClusterScoped: target.ClusterScoped,
	})
}

func (s *KubeScope) Run(ctx context.Context, req Request) (*actions.ActionResult, error) {
	action, ok := req.Action.(actions.KubeAction)
	if !ok {
		return nil, fmt.Errorf("action %q is not a Kubernetes action", req.Action.Name())
	}
	target, ok := req.Target.(actions.ResourceTarget)
	if !ok {
		return nil, fmt.Errorf("target %T is not a Kubernetes resource target", req.Target)
	}

	// Each primitive action gets its own backplane session scoped to exactly the
	// RBAC it needs (least-privilege). Composite actions could merge RBAC rules
	// from all sub-actions and call GetClient once to reuse a single session.
	// (possible design decision for later)
	rbacRules := action.RequiredRBAC(target)
	client, err := s.backplane.GetClient(ctx, req.ClusterID, req.Action.Name(), rbacRules)
	if err != nil {
		return nil, &Failure{Message: "failed to get cluster client", Cause: err}
	}

	clients := actions.KubeClients{Dynamic: client}

	if action.UsesPodExec() {
		podExec, podErr := s.backplane.GetPodExecutor(ctx, req.ClusterID, req.Action.Name(), rbacRules)
		if podErr != nil {
			return nil, &Failure{Message: "failed to get pod executor", Cause: podErr}
		}
		clients.PodExecutor = podExec
	}

	output, err := action.Execute(ctx, clients, actions.KubeActionRequest{
		Target:         target,
		ClusterVersion: req.ClusterVersion,
		Params:         req.Params,
	})
	if err != nil {
		return nil, &Failure{Message: "action execution failed", Cause: err}
	}

	return output, nil
}

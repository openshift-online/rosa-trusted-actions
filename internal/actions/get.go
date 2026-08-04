package actions

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/openshift-online/rosa-trusted-actions-server/internal/backplane"
)

var _ Action = (*GetAction)(nil)

type GetAction struct{}

func NewGetAction() *GetAction { return &GetAction{} }

func (g *GetAction) Name() string { return "get" }

func (g *GetAction) RequiredRBAC(target ResourceTarget) []backplane.RBACRule {
	rule := backplane.RBACRule{
		APIGroups: []string{target.Group},
		Resources: []string{target.Resource},
	}
	if target.Name == "" {
		rule.Verbs = []string{"list"}
	} else {
		rule.Verbs = []string{"get"}
		rule.ResourceNames = []string{target.Name}
	}
	return []backplane.RBACRule{rule}
}

func (g *GetAction) Execute(ctx context.Context, client dynamic.Interface, req ActionRequest) (*ActionResult, error) {
	gvr := schema.GroupVersionResource{
		Group:    req.Target.Group,
		Version:  req.Target.Version,
		Resource: req.Target.Resource,
	}

	rc, err := resourceClient(client, gvr, req.Target)
	if err != nil {
		return nil, err
	}

	if req.Target.Name == "" {
		list, err := rc.List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list %s in %s: %w", req.Target.Resource, scopeLabel(req.Target.Namespace), err)
		}
		return &ActionResult{
			Resources: list.Items,
			Message:   fmt.Sprintf("listed %d %s in %s", len(list.Items), req.Target.Resource, scopeLabel(req.Target.Namespace)),
		}, nil
	}

	obj, err := rc.Get(ctx, req.Target.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get %s/%s in %s: %w", req.Target.Resource, req.Target.Name, scopeLabel(req.Target.Namespace), err)
	}
	return &ActionResult{
		Resources: []unstructured.Unstructured{*obj},
		Message:   fmt.Sprintf("got %s/%s in %s", req.Target.Resource, req.Target.Name, scopeLabel(req.Target.Namespace)),
	}, nil
}

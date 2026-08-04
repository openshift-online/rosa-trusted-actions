package actions

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/openshift-online/rosa-trusted-actions-server/internal/backplane"
)

var _ Action = (*DeleteAction)(nil)

type DeleteAction struct{}

func NewDeleteAction() *DeleteAction { return &DeleteAction{} }

func (d *DeleteAction) Name() string { return "delete" }

func (d *DeleteAction) RequiredRBAC(target ResourceTarget) []backplane.RBACRule {
	rule := backplane.RBACRule{
		APIGroups: []string{target.Group},
		Resources: []string{target.Resource},
		Verbs:     []string{"delete"},
	}
	if target.Name != "" {
		rule.ResourceNames = []string{target.Name}
	}
	return []backplane.RBACRule{rule}
}

func (d *DeleteAction) Execute(ctx context.Context, client dynamic.Interface, req ActionRequest) (*ActionResult, error) {
	if req.Target.Name == "" {
		return nil, fmt.Errorf("delete action requires a resource name")
	}

	gvr := schema.GroupVersionResource{
		Group:    req.Target.Group,
		Version:  req.Target.Version,
		Resource: req.Target.Resource,
	}

	rc := resourceClient(client, gvr, req.Target.Namespace)

	err := rc.Delete(ctx, req.Target.Name, metav1.DeleteOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to delete %s/%s in %s: %w", req.Target.Resource, req.Target.Name, scopeLabel(req.Target.Namespace), err)
	}

	return &ActionResult{
		Message: fmt.Sprintf("deleted %s/%s in %s", req.Target.Resource, req.Target.Name, scopeLabel(req.Target.Namespace)),
	}, nil
}

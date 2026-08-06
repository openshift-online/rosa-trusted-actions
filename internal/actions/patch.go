package actions

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
)

var _ Action = (*PatchAction)(nil)

type PatchAction struct{}

func NewPatchAction() *PatchAction { return &PatchAction{} }

func (p *PatchAction) Name() string { return "patch" }

func (p *PatchAction) RequiredRBAC(target ResourceTarget) []backplane.RBACRule {
	rule := backplane.RBACRule{
		APIGroups: []string{target.Group},
		Resources: []string{target.Resource},
		Verbs:     []string{"patch"},
	}
	if target.Name != "" {
		rule.ResourceNames = []string{target.Name}
	}
	return []backplane.RBACRule{rule}
}

func (p *PatchAction) Execute(ctx context.Context, client dynamic.Interface, req ActionRequest) (*ActionResult, error) {
	if req.Target.Name == "" {
		return nil, fmt.Errorf("patch action requires a resource name")
	}

	patchData, ok := req.Params["patch"]
	if !ok || patchData == "" {
		return nil, fmt.Errorf("patch action requires 'patch' parameter with JSON merge patch body")
	}

	gvr := schema.GroupVersionResource{
		Group:    req.Target.Group,
		Version:  req.Target.Version,
		Resource: req.Target.Resource,
	}

	obj, err := client.Resource(gvr).Namespace(req.Target.Namespace).Patch(
		ctx, req.Target.Name, types.MergePatchType, []byte(patchData), metav1.PatchOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to patch %s/%s in %s: %w", req.Target.Resource, req.Target.Name, req.Target.Namespace, err)
	}

	return &ActionResult{
		Resources: []unstructured.Unstructured{*obj},
		Message:   fmt.Sprintf("patched %s/%s in %s", req.Target.Resource, req.Target.Name, req.Target.Namespace),
	}, nil
}

package actions

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/openshift-online/rosa-trusted-actions-server/internal/backplane"
)

type ResourceTarget struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type ActionRequest struct {
	Target         ResourceTarget
	ClusterVersion string
	Params         map[string]string
}

type ActionResult struct {
	Resources []unstructured.Unstructured
	Message   string
}

type Action interface {
	Name() string
	RequiredRBAC(target ResourceTarget) []backplane.RBACRule
	Execute(ctx context.Context, client dynamic.Interface, req ActionRequest) (*ActionResult, error)
}

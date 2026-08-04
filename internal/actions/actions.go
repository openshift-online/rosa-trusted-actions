package actions

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/openshift-online/rosa-trusted-actions-server/internal/backplane"
)

type ResourceTarget struct {
	Group         string `json:"group"`
	Version       string `json:"version"`
	Resource      string `json:"resource"`
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	ClusterScoped bool   `json:"clusterScoped"`
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

func resourceClient(client dynamic.Interface, gvr schema.GroupVersionResource, target ResourceTarget) (dynamic.ResourceInterface, error) {
	if target.ClusterScoped {
		if target.Namespace != "" {
			return nil, fmt.Errorf("namespace must be empty for cluster-scoped resource %s", target.Resource)
		}
		return client.Resource(gvr), nil
	}
	if target.Namespace == "" {
		return nil, fmt.Errorf("namespace is required for namespaced resource %s", target.Resource)
	}
	return client.Resource(gvr).Namespace(target.Namespace), nil
}

func scopeLabel(namespace string) string {
	if namespace == "" {
		return "cluster scope"
	}
	return namespace
}

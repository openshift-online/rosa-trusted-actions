package backplane

import (
	"context"

	"k8s.io/client-go/dynamic"
)

type RBACRule struct {
	APIGroups     []string `json:"apiGroups"`
	Resources     []string `json:"resources"`
	ResourceNames []string `json:"resourceNames,omitempty"`
	Verbs         []string `json:"verbs"`
}

type PodExecutor interface {
	Exec(ctx context.Context, namespace, pod, container string, command []string) ([]byte, error)
}

type ClientProvider interface {
	GetClient(ctx context.Context, clusterID string, rbacRules []RBACRule) (dynamic.Interface, error)
	GetPodExecutor(ctx context.Context, clusterID string, rbacRules []RBACRule) (PodExecutor, error)
}

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

type ClientProvider interface {
	GetClient(ctx context.Context, clusterID string, rbacRules []RBACRule) (dynamic.Interface, error)
}

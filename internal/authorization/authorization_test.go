package authorization

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func newTestAuthorizer(namespaces, secrets []string) Authorizer {
	return New(logrus.New(), namespaces, secrets)
}

func TestAuthorize_AllowedNamespace(t *testing.T) {
	authz := newTestAuthorizer([]string{"openshift-monitoring", "openshift-logging"}, nil)

	result := authz.Authorize(Request{
		Namespace:    "openshift-monitoring",
		ResourceType: "configmaps",
		ResourceName: "cluster-config",
	})

	if !result.Allowed {
		t.Errorf("expected allowed, got denied: %s", result.Reason)
	}
}

func TestAuthorize_DeniedNamespace(t *testing.T) {
	authz := newTestAuthorizer([]string{"openshift-monitoring"}, nil)

	result := authz.Authorize(Request{
		Namespace:    "customer-namespace",
		ResourceType: "pods",
		ResourceName: "my-pod",
	})

	if result.Allowed {
		t.Error("expected denied for customer namespace, got allowed")
	}
	if result.Reason == "" {
		t.Error("expected denial reason, got empty string")
	}
}

func TestAuthorize_ClusterScoped(t *testing.T) {
	authz := newTestAuthorizer([]string{"openshift-monitoring"}, nil)

	result := authz.Authorize(Request{
		Namespace:     "",
		ResourceType:  "namespaces",
		ResourceName:  "kube-system",
		ClusterScoped: true,
	})

	if !result.Allowed {
		t.Errorf("expected cluster-scoped request to be allowed, got denied: %s", result.Reason)
	}
}

func TestAuthorize_EmptyNamespace_NamespacedResource(t *testing.T) {
	authz := newTestAuthorizer([]string{"openshift-monitoring"}, nil)

	result := authz.Authorize(Request{
		Namespace:    "",
		ResourceType: "configmaps",
		ResourceName: "cluster-config",
	})

	if result.Allowed {
		t.Error("expected denied for namespaced resource with empty namespace, got allowed")
	}
}

func TestAuthorize_SecretDeniedByDefault(t *testing.T) {
	authz := newTestAuthorizer([]string{"openshift-monitoring"}, nil)

	result := authz.Authorize(Request{
		Namespace:    "openshift-monitoring",
		ResourceType: "secrets",
		ResourceName: "alertmanager-config",
	})

	if result.Allowed {
		t.Error("expected secrets to be denied by default, got allowed")
	}
}

func TestAuthorize_SecretAllowed(t *testing.T) {
	authz := newTestAuthorizer(
		[]string{"openshift-monitoring"},
		[]string{"openshift-monitoring/alertmanager-config"},
	)

	result := authz.Authorize(Request{
		Namespace:    "openshift-monitoring",
		ResourceType: "secrets",
		ResourceName: "alertmanager-config",
	})

	if !result.Allowed {
		t.Errorf("expected secret in allowlist to be allowed, got denied: %s", result.Reason)
	}
}

func TestAuthorize_SecretInWrongNamespace(t *testing.T) {
	authz := newTestAuthorizer(
		[]string{"openshift-monitoring", "openshift-logging"},
		[]string{"openshift-monitoring/alertmanager-config"},
	)

	result := authz.Authorize(Request{
		Namespace:    "openshift-logging",
		ResourceType: "secrets",
		ResourceName: "alertmanager-config",
	})

	if result.Allowed {
		t.Error("expected secret in wrong namespace to be denied, got allowed")
	}
}

func TestAuthorize_ListSecretsDenied(t *testing.T) {
	authz := newTestAuthorizer([]string{"openshift-monitoring"}, nil)

	result := authz.Authorize(Request{
		Namespace:    "openshift-monitoring",
		ResourceType: "secrets",
		ResourceName: "",
	})

	if result.Allowed {
		t.Error("expected listing secrets to be denied, got allowed")
	}
}

func TestAuthorize_EmptyAllowlists(t *testing.T) {
	authz := newTestAuthorizer(nil, nil)

	result := authz.Authorize(Request{
		Namespace:    "openshift-monitoring",
		ResourceType: "pods",
		ResourceName: "my-pod",
	})

	if result.Allowed {
		t.Error("expected denied with empty allowlists, got allowed")
	}
}

func TestAuthorize_ClusterScoped_EmptyAllowlists(t *testing.T) {
	authz := newTestAuthorizer(nil, nil)

	result := authz.Authorize(Request{
		Namespace:     "",
		ResourceType:  "nodes",
		ResourceName:  "node-1",
		ClusterScoped: true,
	})

	if !result.Allowed {
		t.Errorf("expected cluster-scoped request allowed even with empty allowlists, got denied: %s", result.Reason)
	}
}

func TestAuthorize_ClusterScoped_SecretsBlocked(t *testing.T) {
	authz := newTestAuthorizer([]string{"openshift-monitoring"}, []string{"openshift-monitoring/my-secret"})

	result := authz.Authorize(Request{
		Namespace:     "",
		ResourceType:  "secrets",
		ResourceName:  "my-secret",
		ClusterScoped: true,
	})

	if result.Allowed {
		t.Error("expected cluster-scoped secret request to be denied, got allowed")
	}
}

func TestAuthorize_NonSecretInAllowedNamespace(t *testing.T) {
	authz := newTestAuthorizer([]string{"openshift-logging"}, nil)

	tests := []struct {
		name         string
		resourceType string
	}{
		{"pods", "pods"},
		{"configmaps", "configmaps"},
		{"deployments", "deployments"},
		{"services", "services"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := authz.Authorize(Request{
				Namespace:    "openshift-logging",
				ResourceType: tt.resourceType,
				ResourceName: "test-resource",
			})
			if !result.Allowed {
				t.Errorf("expected %s to be allowed, got denied: %s", tt.resourceType, result.Reason)
			}
		})
	}
}

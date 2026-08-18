package actions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
)

var _ Action = (*GetPullSecretEmailAction)(nil)

var secretsGVR = schema.GroupVersionResource{
	Group: "", Version: "v1", Resource: "secrets",
}

const (
	pullSecretNamespace = "openshift-config" // #nosec G101
	pullSecretName      = "pull-secret"
	pullSecretRegistry  = "cloud.openshift.com"
)

type GetPullSecretEmailAction struct{}

func NewGetPullSecretEmailAction() *GetPullSecretEmailAction {
	return &GetPullSecretEmailAction{}
}

func (g *GetPullSecretEmailAction) Name() string      { return "get-pull-secret-email" }
func (g *GetPullSecretEmailAction) UsesPodExec() bool { return false }

func (g *GetPullSecretEmailAction) RequiredRBAC(_ ResourceTarget) []backplane.RBACRule {
	return []backplane.RBACRule{
		{
			APIGroups:     []string{""},
			Resources:     []string{"secrets"},
			ResourceNames: []string{pullSecretName},
			Verbs:         []string{"get"},
		},
	}
}

func (g *GetPullSecretEmailAction) Execute(ctx context.Context, clients Clients, _ ActionRequest) (*ActionResult, error) {
	secret, err := clients.Dynamic.Resource(secretsGVR).Namespace(pullSecretNamespace).Get(ctx, pullSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s: %w", pullSecretNamespace, pullSecretName, err)
	}

	email, err := extractEmail(secret)
	if err != nil {
		return nil, err
	}

	return &ActionResult{
		Resources: []unstructured.Unstructured{
			{Object: map[string]interface{}{"email": email}},
		},
		Message: "retrieved pull secret email",
	}, nil
}

func extractEmail(secret *unstructured.Unstructured) (string, error) {
	data, ok := secret.Object["data"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("secret %s/%s has no data field", pullSecretNamespace, pullSecretName)
	}

	encoded, ok := data[".dockerconfigjson"].(string)
	if !ok || encoded == "" {
		return "", fmt.Errorf("secret %s/%s has no .dockerconfigjson key", pullSecretNamespace, pullSecretName)
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to base64-decode .dockerconfigjson: %w", err)
	}

	var dockerConfig struct {
		Auths map[string]struct {
			Email string `json:"email"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(decoded, &dockerConfig); err != nil {
		return "", fmt.Errorf("failed to parse .dockerconfigjson: %w", err)
	}

	entry, ok := dockerConfig.Auths[pullSecretRegistry]
	if !ok {
		return "", fmt.Errorf("no auth entry for %s in pull secret", pullSecretRegistry)
	}

	if entry.Email == "" {
		return "", fmt.Errorf("no email found in %s auth entry", pullSecretRegistry)
	}

	return entry.Email, nil
}

package backplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"

	bpclient "github.com/openshift/backplane-api/pkg/client"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

var _ ClientProvider = (*BackplaneProvider)(nil)

type BackplaneProvider struct {
	logger    *logrus.Logger
	baseURL   string
	tokenFunc func(ctx context.Context) (string, error)
	bpClient  *bpclient.Client
}

func NewBackplaneProvider(logger *logrus.Logger, baseURL string, tokenFunc func(ctx context.Context) (string, error)) (*BackplaneProvider, error) {
	client, err := bpclient.NewClient(baseURL,
		bpclient.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			token, err := tokenFunc(ctx)
			if err != nil {
				return fmt.Errorf("failed to get auth token: %w", err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create backplane client: %w", err)
	}

	return &BackplaneProvider{
		logger:    logger,
		baseURL:   baseURL,
		tokenFunc: tokenFunc,
		bpClient:  client,
	}, nil
}

func (b *BackplaneProvider) requestAccessAndConfig(ctx context.Context, clusterID, actionName string, rbacRules []RBACRule) (*rest.Config, error) {
	result, err := b.requestAccess(ctx, clusterID, actionName, rbacRules)
	if err != nil {
		return nil, fmt.Errorf("failed to request backplane access: %w", err)
	}

	base, err := url.Parse(b.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	ref, err := url.Parse(result.ProxyUri)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URI from backplane: %w", err)
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != base.Scheme || resolved.Host != base.Host {
		return nil, fmt.Errorf("backplane returned proxy URI targeting a different origin: %s", resolved)
	}
	proxyURL := resolved.String()

	config := &rest.Config{
		Host: proxyURL,
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
			return &bearerTransport{
				base:        rt,
				tokenFunc:   b.tokenFunc,
				trustedHost: resolved.Host,
			}
		},
	}

	b.logger.WithFields(logrus.Fields{
		"cluster_id":  clusterID,
		"instance_id": result.InstanceId,
		"proxy_url":   proxyURL,
		"expiry":      result.Expiry,
	}).Debug("backplane access established")

	return config, nil
}

func (b *BackplaneProvider) GetClient(ctx context.Context, clusterID, actionName string, rbacRules []RBACRule) (dynamic.Interface, error) {
	config, err := b.requestAccessAndConfig(ctx, clusterID, actionName, rbacRules)
	if err != nil {
		return nil, err
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client for proxy: %w", err)
	}

	return client, nil
}

func (b *BackplaneProvider) GetPodExecutor(ctx context.Context, clusterID, actionName string, rbacRules []RBACRule) (PodExecutor, error) {
	config, err := b.requestAccessAndConfig(ctx, clusterID, actionName, rbacRules)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset for proxy: %w", err)
	}

	return &backplanePodExecutor{config: config, clientset: clientset}, nil
}

func (b *BackplaneProvider) requestAccess(ctx context.Context, clusterID, actionName string, rbacRules []RBACRule) (*bpclient.CreateTrustedActionResult, error) {
	reqBody := buildCreateRequest(actionName, rbacRules)

	resp, err := b.bpClient.CreateTrustedAction(ctx, clusterID, nil, reqBody)
	if err != nil {
		return nil, fmt.Errorf("backplane request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("backplane returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result bpclient.CreateTrustedActionResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode backplane response: %w", err)
	}

	return &result, nil
}

func buildCreateRequest(actionName string, rbacRules []RBACRule) bpclient.CreateTrustedActionRequest {
	var clusterRoleRules []bpclient.PolicyRule
	rolesByNS := make(map[string][]bpclient.PolicyRule)

	for _, rule := range rbacRules {
		pr := toBPPolicyRule(rule)
		if rule.Namespace == "" {
			clusterRoleRules = append(clusterRoleRules, pr)
		} else {
			rolesByNS[rule.Namespace] = append(rolesByNS[rule.Namespace], pr)
		}
	}

	namespaces := make([]string, 0, len(rolesByNS))
	for ns := range rolesByNS {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	roles := make([]bpclient.RoleRbacDecl, 0, len(namespaces))
	for _, ns := range namespaces {
		ns := ns
		rules := rolesByNS[ns]
		roles = append(roles, bpclient.RoleRbacDecl{Namespace: &ns, Rules: &rules})
	}

	return bpclient.CreateTrustedActionRequest{
		Name:               actionName,
		CustomerDataAccess: false,
		Rbac: bpclient.TrustedActionRbacDecl{
			ClusterRoleRules: clusterRoleRules,
			Roles:            roles,
		},
	}
}

func toBPPolicyRule(rule RBACRule) bpclient.PolicyRule {
	pr := bpclient.PolicyRule{
		ApiGroups: &rule.APIGroups,
		Resources: &rule.Resources,
		Verbs:     &rule.Verbs,
	}
	if len(rule.ResourceNames) > 0 {
		pr.ResourceNames = &rule.ResourceNames
	}
	return pr
}

type bearerTransport struct {
	base        http.RoundTripper
	tokenFunc   func(ctx context.Context) (string, error)
	trustedHost string
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if t.trustedHost == "" || req.URL.Host == "" || req.URL.Host == t.trustedHost {
		token, err := t.tokenFunc(req.Context())
		if err != nil {
			return nil, fmt.Errorf("failed to get auth token: %w", err)
		}
		clone.Header.Set("Authorization", "Bearer "+token)
	}
	return t.base.RoundTrip(clone)
}

type backplanePodExecutor struct {
	config    *rest.Config
	clientset kubernetes.Interface
}

func (e *backplanePodExecutor) Exec(ctx context.Context, namespace, pod, container string, command []string) ([]byte, error) {
	req := e.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(e.config, "POST", req.URL())
	if err != nil {
		return nil, fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return nil, fmt.Errorf("exec failed: %w (stderr: %s)", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

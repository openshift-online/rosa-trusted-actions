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
	"time"

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
	logger     *logrus.Logger
	baseURL    string
	tokenFunc  func(ctx context.Context) (string, error)
	httpClient *http.Client
}

func NewBackplaneProvider(logger *logrus.Logger, baseURL string, tokenFunc func(ctx context.Context) (string, error)) *BackplaneProvider {
	parsed, _ := url.Parse(baseURL)

	return &BackplaneProvider{
		logger:    logger,
		baseURL:   baseURL,
		tokenFunc: tokenFunc,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if parsed != nil && (req.URL.Scheme != parsed.Scheme || req.URL.Host != parsed.Host) {
					return fmt.Errorf("refusing redirect to %s (expected %s://%s)", req.URL, parsed.Scheme, parsed.Host)
				}
				return nil
			},
		},
	}
}

type trustedActionRequest struct {
	Name               string            `json:"name"`
	CustomerDataAccess bool              `json:"customerDataAccess"`
	Rbac               trustedActionRbac `json:"rbac"`
}

type trustedActionRbac struct {
	ClusterRoleRules []policyRule `json:"clusterRoleRules"`
	Roles            []roleDecl   `json:"roles"`
}

type roleDecl struct {
	Namespace string       `json:"namespace"`
	Rules     []policyRule `json:"rules"`
}

type policyRule struct {
	APIGroups     []string `json:"apiGroups"`
	Resources     []string `json:"resources"`
	ResourceNames []string `json:"resourceNames,omitempty"`
	Verbs         []string `json:"verbs"`
}

type trustedActionResponse struct {
	ProxyUri   string `json:"proxyUri"`
	InstanceId string `json:"instanceId"`
	Expiry     string `json:"expiry"`
}

func (b *BackplaneProvider) requestAccessAndConfig(ctx context.Context, clusterID, actionName string, rbacRules []RBACRule) (*rest.Config, error) {
	resp, err := b.requestAccess(ctx, clusterID, actionName, rbacRules)
	if err != nil {
		return nil, fmt.Errorf("failed to request backplane access: %w", err)
	}

	base, err := url.Parse(b.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	ref, err := url.Parse(resp.ProxyUri)
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
		"instance_id": resp.InstanceId,
		"proxy_url":   proxyURL,
		"expiry":      resp.Expiry,
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

func (b *BackplaneProvider) requestAccess(ctx context.Context, clusterID, actionName string, rbacRules []RBACRule) (*trustedActionResponse, error) {
	reqBody := buildTrustedActionRequest(actionName, rbacRules)

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/backplane/trustedactions/%s", b.baseURL, clusterID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	token, err := b.tokenFunc(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get auth token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("backplane request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("backplane returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result trustedActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode backplane response: %w", err)
	}

	return &result, nil
}

func buildTrustedActionRequest(actionName string, rbacRules []RBACRule) trustedActionRequest {
	var clusterRoleRules []policyRule
	rolesByNS := make(map[string][]policyRule)

	for _, rule := range rbacRules {
		pr := policyRule{
			APIGroups:     rule.APIGroups,
			Resources:     rule.Resources,
			ResourceNames: rule.ResourceNames,
			Verbs:         rule.Verbs,
		}
		if rule.Namespace == "" {
			clusterRoleRules = append(clusterRoleRules, pr)
		} else {
			rolesByNS[rule.Namespace] = append(rolesByNS[rule.Namespace], pr)
		}
	}

	// Deterministic ordering so backplane requests are consistent across runs.
	namespaces := make([]string, 0, len(rolesByNS))
	for ns := range rolesByNS {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	roles := make([]roleDecl, 0, len(namespaces))
	for _, ns := range namespaces {
		roles = append(roles, roleDecl{Namespace: ns, Rules: rolesByNS[ns]})
	}

	return trustedActionRequest{
		Name:               actionName,
		CustomerDataAccess: false,
		Rbac: trustedActionRbac{
			ClusterRoleRules: clusterRoleRules,
			Roles:            roles,
		},
	}
}

type bearerTransport struct {
	base       http.RoundTripper
	tokenFunc  func(ctx context.Context) (string, error)
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

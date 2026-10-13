//go:build integration

package actiontest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.com/openshift-online/rosa-trusted-actions/internal/audit"
	"github.com/openshift-online/rosa-trusted-actions/internal/authorization"
	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
	"github.com/openshift-online/rosa-trusted-actions/internal/executor"
)

const (
	defaultMockURL  = "http://localhost:8087"
	defaultProxyURL = "http://localhost:8088"
	clusterC1       = "00000000-0000-0000-0000-000000000001"
)

var (
	mockBaseURL    string
	mockAvailable  bool
	proxyBaseURL   string
	proxyAvailable bool
	kubeconfigB64  string
)

func TestMain(m *testing.M) {
	client := &http.Client{Timeout: 2 * time.Second}

	mockBaseURL = os.Getenv("MOCK_BACKPLANE_URL")
	if mockBaseURL == "" {
		mockBaseURL = defaultMockURL
	}
	if resp, err := client.Get(mockBaseURL); err == nil {
		resp.Body.Close()
		mockAvailable = true
	}

	proxyBaseURL = os.Getenv("PROXY_BACKPLANE_URL")
	if proxyBaseURL == "" {
		proxyBaseURL = defaultProxyURL
	}
	loadKubeconfig()
	if kubeconfigB64 != "" {
		if resp, err := client.Get(proxyBaseURL); err == nil {
			resp.Body.Close()
			proxyAvailable = true
		}
	}

	switch selected := os.Getenv("TEST_BACKPLANE"); selected {
	case "mock":
		if !mockAvailable {
			fmt.Fprintf(os.Stderr, "TEST_BACKPLANE=mock but mock backplane is not reachable at %s\n", mockBaseURL)
			os.Exit(1)
		}
	case "proxy":
		if !proxyAvailable {
			fmt.Fprintf(os.Stderr, "TEST_BACKPLANE=proxy but proxy backplane is not reachable at %s\n", proxyBaseURL)
			os.Exit(1)
		}
	default:
		if !mockAvailable && !proxyAvailable {
			fmt.Fprintf(os.Stderr, "no backplane available\n")
			fmt.Fprintf(os.Stderr, "  mock:  start with 'make mock-backplane'\n")
			fmt.Fprintf(os.Stderr, "  proxy: set KUBECONFIG_B64 or KUBECONFIG, start with 'make proxy-backplane'\n")
			os.Exit(1)
		}
	}

	os.Exit(m.Run())
}

func loadKubeconfig() {
	kubeconfigB64 = os.Getenv("KUBECONFIG_B64")
	if kubeconfigB64 != "" {
		return
	}
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		return
	}
	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return
	}
	kubeconfigB64 = base64.StdEncoding.EncodeToString(data)
}

func requireMock(t *testing.T) {
	t.Helper()
	if !mockAvailable {
		t.Skip("mock backplane not available")
	}
}

func requireProxy(t *testing.T) {
	t.Helper()
	if !proxyAvailable {
		t.Skip("proxy backplane not available")
	}
}

// mockRBACRequest is the request body the Imposter mock expects at
// POST /backplane/trustedactions/{clusterId}.
type mockRBACRequest struct {
	Name               string   `json:"name"`
	CustomerDataAccess bool     `json:"customerDataAccess"`
	RBAC               mockRBAC `json:"rbac"`
}

type mockRBAC struct {
	ClusterRoleRules []backplane.RBACRule `json:"clusterRoleRules"`
	Roles            []interface{}        `json:"roles"`
}

type mockCreateResponse struct {
	ProxyURI   string `json:"proxyUri"`
	InstanceID string `json:"instanceId"`
}

type mockErrorResponse struct {
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
}

// createMockSession creates a trusted action on the mock backplane and returns
// a dynamic.Interface that proxies through the mock. The session is cleaned up
// when the test finishes.
func createMockSession(t *testing.T, clusterID string, rbacRules []backplane.RBACRule) dynamic.Interface {
	t.Helper()
	requireMock(t)

	reqBody := mockRBACRequest{
		Name:               t.Name(),
		CustomerDataAccess: false,
		RBAC: mockRBAC{
			ClusterRoleRules: rbacRules,
			Roles:            []interface{}{},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal mock request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/backplane/trustedactions/%s", mockBaseURL, clusterID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("create mock request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create mock session: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mock backplane returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result mockCreateResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode mock response: %v", err)
	}

	proxyURL := mockBaseURL + result.ProxyURI
	config := &rest.Config{Host: proxyURL}
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatalf("create dynamic client: %v", err)
	}

	t.Cleanup(func() {
		delURL := fmt.Sprintf("%s/backplane/trustedactions/%s/%s", mockBaseURL, clusterID, result.InstanceID)
		req, err := http.NewRequest(http.MethodDelete, delURL, nil)
		if err != nil {
			t.Errorf("construct cleanup request: %v", err)
			return
		}
		if delResp, err := http.DefaultClient.Do(req); err == nil {
			delResp.Body.Close()
		}
	})

	return dynClient
}

// createMockSessionExpectError calls the mock's create endpoint and expects it
// to fail with the given status code. Returns the error message.
func createMockSessionExpectError(t *testing.T, clusterID string, rbacRules []backplane.RBACRule, wantStatus int) string {
	t.Helper()
	requireMock(t)

	reqBody := mockRBACRequest{
		Name:               t.Name(),
		CustomerDataAccess: false,
		RBAC: mockRBAC{
			ClusterRoleRules: rbacRules,
			Roles:            []interface{}{},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal mock request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/backplane/trustedactions/%s", mockBaseURL, clusterID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("create mock request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create mock session: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("expected status %d, got %d: %s", wantStatus, resp.StatusCode, string(respBody))
	}

	var errResp mockErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return errResp.Message
}

// mockRole is a namespace-scoped RBAC role for the Imposter mock.
type mockRole struct {
	Namespace string              `json:"namespace"`
	Rules     []backplane.RBACRule `json:"rules"`
}

// createMockSessionWithRoles creates a trusted action using namespace-scoped
// roles instead of (or in addition to) clusterRoleRules. This tests the mock's
// Role-vs-ClusterRole RBAC distinction.
func createMockSessionWithRoles(t *testing.T, clusterID string, clusterRules []backplane.RBACRule, roles []mockRole) dynamic.Interface {
	t.Helper()
	requireMock(t)

	type rbacWithRoles struct {
		ClusterRoleRules []backplane.RBACRule `json:"clusterRoleRules"`
		Roles            []mockRole           `json:"roles"`
	}

	type reqWithRoles struct {
		Name               string       `json:"name"`
		CustomerDataAccess bool         `json:"customerDataAccess"`
		RBAC               rbacWithRoles `json:"rbac"`
	}

	reqBody := reqWithRoles{
		Name:               t.Name(),
		CustomerDataAccess: false,
		RBAC: rbacWithRoles{
			ClusterRoleRules: clusterRules,
			Roles:            roles,
		},
	}
	if reqBody.RBAC.ClusterRoleRules == nil {
		reqBody.RBAC.ClusterRoleRules = []backplane.RBACRule{}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal mock request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/backplane/trustedactions/%s", mockBaseURL, clusterID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("create mock request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create mock session: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mock backplane returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result mockCreateResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode mock response: %v", err)
	}

	proxyURL := mockBaseURL + result.ProxyURI
	config := &rest.Config{Host: proxyURL}
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatalf("create dynamic client: %v", err)
	}

	t.Cleanup(func() {
		delURL := fmt.Sprintf("%s/backplane/trustedactions/%s/%s", mockBaseURL, clusterID, result.InstanceID)
		req, err := http.NewRequest(http.MethodDelete, delURL, nil)
		if err != nil {
			t.Errorf("construct cleanup request: %v", err)
			return
		}
		if delResp, err := http.DefaultClient.Do(req); err == nil {
			delResp.Body.Close()
		}
	})

	return dynClient
}

// newTestExecutor builds an executor wired to the mock backplane.
func newTestExecutor(t *testing.T, allowedNS []string, allowedSecrets []string) (*executor.Executor, *audit.MockLogger) {
	t.Helper()
	requireMock(t)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	authorizer := authorization.New(logger, allowedNS, allowedSecrets)
	auditor := audit.NewMockLogger(logger)
	provider := newMockBackplaneProvider(t)
	return executor.New(logger, authorizer, auditor, provider), auditor
}

// mockBackplaneProvider implements backplane.ClientProvider by talking to the
// Imposter mock. Used in executor-level tests.
type mockBackplaneProvider struct {
	t *testing.T
}

var _ backplane.ClientProvider = (*mockBackplaneProvider)(nil)

func newMockBackplaneProvider(t *testing.T) *mockBackplaneProvider {
	return &mockBackplaneProvider{t: t}
}

func (m *mockBackplaneProvider) GetClient(ctx context.Context, clusterID string, rbacRules []backplane.RBACRule) (dynamic.Interface, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	reqBody := mockRBACRequest{
		Name:               m.t.Name(),
		CustomerDataAccess: false,
		RBAC: mockRBAC{
			ClusterRoleRules: rbacRules,
			Roles:            []interface{}{},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal mock request: %w", err)
	}

	url := fmt.Sprintf("%s/backplane/trustedactions/%s", mockBaseURL, clusterID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mock backplane request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mock backplane returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result mockCreateResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode mock response: %w", err)
	}

	m.t.Cleanup(func() {
		delURL := fmt.Sprintf("%s/backplane/trustedactions/%s/%s", mockBaseURL, clusterID, result.InstanceID)
		req, err := http.NewRequest(http.MethodDelete, delURL, nil)
		if err != nil {
			return
		}
		if delResp, err := http.DefaultClient.Do(req); err == nil {
			delResp.Body.Close()
		}
	})

	proxyURL := mockBaseURL + result.ProxyURI
	config := &rest.Config{Host: proxyURL}
	return dynamic.NewForConfig(config)
}

func (m *mockBackplaneProvider) GetPodExecutor(_ context.Context, _ string, _ []backplane.RBACRule) (backplane.PodExecutor, error) {
	return nil, fmt.Errorf("mock backplane does not support pod execution")
}

// ---------------------------------------------------------------------------
// proxy backplane helpers
// ---------------------------------------------------------------------------

type proxyRegisterRequest struct {
	Name               string `json:"name"`
	CustomerDataAccess string `json:"customerDataAccess"`
	Kubeconfig         string `json:"kubeconfig"`
}

type proxyRegisterResponse struct {
	ProxyURI   string `json:"proxyUri"`
	InstanceID string `json:"instanceId"`
}

func createProxySession(t *testing.T, clusterID string) dynamic.Interface {
	t.Helper()
	requireProxy(t)

	reqBody := proxyRegisterRequest{
		Name:       t.Name(),
		Kubeconfig: kubeconfigB64,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal proxy request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/backplane/trustedactions/%s", proxyBaseURL, clusterID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("create proxy request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create proxy session: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy backplane returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result proxyRegisterResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode proxy response: %v", err)
	}

	config := &rest.Config{Host: result.ProxyURI}
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatalf("create dynamic client: %v", err)
	}

	t.Cleanup(func() {
		delURL := fmt.Sprintf("%s/backplane/trustedactions/%s/%s", proxyBaseURL, clusterID, result.InstanceID)
		req, err := http.NewRequest(http.MethodDelete, delURL, nil)
		if err != nil {
			t.Errorf("construct cleanup request: %v", err)
			return
		}
		if delResp, err := http.DefaultClient.Do(req); err == nil {
			delResp.Body.Close()
		}
	})

	return dynClient
}

type proxyBackplaneProvider struct {
	t *testing.T
}

var _ backplane.ClientProvider = (*proxyBackplaneProvider)(nil)

func newProxyBackplaneProvider(t *testing.T) *proxyBackplaneProvider {
	return &proxyBackplaneProvider{t: t}
}

func (p *proxyBackplaneProvider) GetClient(ctx context.Context, clusterID string, _ []backplane.RBACRule) (dynamic.Interface, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	reqBody := proxyRegisterRequest{
		Name:       p.t.Name(),
		Kubeconfig: kubeconfigB64,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal proxy request: %w", err)
	}

	url := fmt.Sprintf("%s/backplane/trustedactions/%s", proxyBaseURL, clusterID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxy backplane request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy backplane returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result proxyRegisterResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode proxy response: %w", err)
	}

	p.t.Cleanup(func() {
		delURL := fmt.Sprintf("%s/backplane/trustedactions/%s/%s", proxyBaseURL, clusterID, result.InstanceID)
		req, err := http.NewRequest(http.MethodDelete, delURL, nil)
		if err != nil {
			return
		}
		if delResp, err := http.DefaultClient.Do(req); err == nil {
			delResp.Body.Close()
		}
	})

	config := &rest.Config{Host: result.ProxyURI}
	return dynamic.NewForConfig(config)
}

func (p *proxyBackplaneProvider) GetPodExecutor(_ context.Context, _ string, _ []backplane.RBACRule) (backplane.PodExecutor, error) {
	return nil, fmt.Errorf("proxy backplane does not support pod execution")
}

func newTestExecutorWithProxy(t *testing.T, allowedNS []string, allowedSecrets []string) (*executor.Executor, *audit.MockLogger) {
	t.Helper()
	requireProxy(t)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	authorizer := authorization.New(logger, allowedNS, allowedSecrets)
	auditor := audit.NewMockLogger(logger)
	provider := newProxyBackplaneProvider(t)
	return executor.New(logger, authorizer, auditor, provider), auditor
}

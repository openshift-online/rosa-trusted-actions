package rta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

const basePath = "/api/v0/trusted-actions"

type Client struct {
	base      string
	token     string
	http      *http.Client
	userAgent string
}

type ClientOption func(*Client)

func WithToken(token string) ClientOption {
	return func(c *Client) { c.token = token }
}

func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) { c.http = hc }
}

func NewClient(serverURL string, opts ...ClientOption) (*Client, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("server URL is required; set --server or RTA_SERVER")
	}
	serverURL = strings.TrimRight(serverURL, "/")

	c := &Client{
		base:      serverURL + basePath,
		http:      &http.Client{CheckRedirect: rejectHTTPSDowngrade},
		userAgent: userAgent(),
	}
	for _, o := range opts {
		o(c)
	}
	if c.token != "" && !isSecureOrLoopback(serverURL) {
		return nil, fmt.Errorf("refusing to send credentials over plain HTTP to %s; use HTTPS or a loopback address", serverURL)
	}
	return c, nil
}

func rejectHTTPSDowngrade(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("refusing redirect from HTTPS to %s: would expose credentials", req.URL)
	}
	return nil
}

func isSecureOrLoopback(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func userAgent() string {
	info := getVersionInfo()
	return "rta/" + info.Version + " (" + info.Platform + ")"
}

func (c *Client) Catalog(ctx context.Context) (*openapi.TrustedActionCatalog, error) {
	var catalog openapi.TrustedActionCatalog
	if err := c.get(ctx, "/", nil, &catalog); err != nil {
		return nil, err
	}
	return &catalog, nil
}

func (c *Client) Describe(ctx context.Context, action string) (*openapi.TrustedAction, error) {
	var ta openapi.TrustedAction
	if err := c.get(ctx, "/"+url.PathEscape(action), nil, &ta); err != nil {
		return nil, err
	}
	return &ta, nil
}

func (c *Client) CreateExecution(ctx context.Context, action string, req openapi.ExecutionRequest) (*openapi.Execution, error) {
	var exec openapi.Execution
	if err := c.post(ctx, "/"+url.PathEscape(action)+"/run", req, &exec); err != nil {
		return nil, err
	}
	return &exec, nil
}

func (c *Client) ListExecutions(ctx context.Context, params url.Values) (*openapi.ExecutionList, error) {
	var list openapi.ExecutionList
	if err := c.get(ctx, "/runs", params, &list); err != nil {
		return nil, err
	}
	return &list, nil
}

func (c *Client) GetExecution(ctx context.Context, id string) (*openapi.Execution, error) {
	var exec openapi.Execution
	if err := c.get(ctx, "/runs/"+url.PathEscape(id), nil, &exec); err != nil {
		return nil, err
	}
	return &exec, nil
}

func (c *Client) GetExecutionOutput(ctx context.Context, id string) (*openapi.ExecutionOutput, error) {
	var output openapi.ExecutionOutput
	if err := c.get(ctx, "/runs/"+url.PathEscape(id)+"/output", nil, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

func (c *Client) ListAuditEntries(ctx context.Context, params url.Values) (*openapi.AuditList, error) {
	var list openapi.AuditList
	if err := c.get(ctx, "/audit", params, &list); err != nil {
		return nil, err
	}
	return &list, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s %s: read response: %w", req.Method, req.URL.Path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseAPIError(req, resp.StatusCode, data)
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s %s: decode response: %w", req.Method, req.URL.Path, err)
		}
	}
	return nil
}

type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Code       string
	Reason     string
}

func (e *APIError) Error() string {
	status := http.StatusText(e.StatusCode)
	if e.Code != "" {
		status = e.Code
	}
	if e.Reason != "" {
		return fmt.Sprintf("%s %s: %d %s: %s", e.Method, e.Path, e.StatusCode, status, e.Reason)
	}
	return fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.StatusCode, status)
}

func parseAPIError(req *http.Request, statusCode int, body []byte) error {
	apiErr := &APIError{
		Method:     req.Method,
		Path:       req.URL.Path,
		StatusCode: statusCode,
	}
	var errResp openapi.Error
	if json.Unmarshal(body, &errResp) == nil && errResp.Code != "" {
		apiErr.Code = errResp.Code
		apiErr.Reason = errResp.Reason
	} else if trimmed := strings.TrimSpace(string(body)); len(trimmed) > 0 && len(trimmed) <= 256 {
		apiErr.Reason = trimmed
	}
	return apiErr
}

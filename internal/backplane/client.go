package backplane

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var _ ClientProvider = (*BackplaneProvider)(nil)

// TODO(ROSAENG-61966): This backplane client is NOT tested against a real backplane instance.
// Do not trust it in production until ROSAENG-61966 integration tests are complete.
type BackplaneProvider struct {
	logger       *logrus.Logger
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client
}

func NewBackplaneProvider(logger *logrus.Logger, baseURL, clientID, clientSecret string) *BackplaneProvider {
	return &BackplaneProvider{
		logger:       logger,
		baseURL:      baseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

type trustedActionRequest struct {
	RBACRules          []RBACRule `json:"rbacRules"`
	CustomerDataAccess bool       `json:"customerDataAccess"`
}

type trustedActionResponse struct {
	InstanceID string `json:"instanceId"`
}

func (b *BackplaneProvider) GetClient(ctx context.Context, clusterID string, rbacRules []RBACRule) (dynamic.Interface, error) {
	instanceID, err := b.requestAccess(ctx, clusterID, rbacRules)
	if err != nil {
		return nil, fmt.Errorf("failed to request backplane access: %w", err)
	}

	proxyURL := fmt.Sprintf("%s/backplane/remediate/%s/%s", b.baseURL, clusterID, instanceID)

	config := &rest.Config{
		Host: proxyURL,
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
			return &hmacTransport{
				base:         rt,
				clientID:     b.clientID,
				clientSecret: b.clientSecret,
			}
		},
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client for proxy: %w", err)
	}

	b.logger.WithFields(logrus.Fields{
		"cluster_id":  clusterID,
		"instance_id": instanceID,
		"proxy_url":   proxyURL,
	}).Debug("backplane access established")

	return client, nil
}

func (b *BackplaneProvider) requestAccess(ctx context.Context, clusterID string, rbacRules []RBACRule) (string, error) {
	reqBody := trustedActionRequest{
		RBACRules:          rbacRules,
		CustomerDataAccess: false,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/backplane/trustedaction/%s", b.baseURL, clusterID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	signRequest(req, bodyBytes, b.clientID, b.clientSecret)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("backplane request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("backplane returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result trustedActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode backplane response: %w", err)
	}

	return result.InstanceID, nil
}

func signRequest(req *http.Request, body []byte, clientID, secret string) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	tsBytes := []byte(timestamp)
	payload := make([]byte, 0, len(body)+len(tsBytes))
	payload = append(payload, body...)
	payload = append(payload, tsBytes...)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	signature := hex.EncodeToString(mac.Sum(nil))

	req.Header.Set("X-Caller-ID", clientID)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", signature)
}

type hmacTransport struct {
	base         http.RoundTripper
	clientID     string
	clientSecret string
}

func (t *hmacTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())

	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(req.Body, 1<<20))
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		clone.Body = io.NopCloser(bytes.NewReader(body))
	}

	signRequest(clone, body, t.clientID, t.clientSecret)

	return t.base.RoundTrip(clone)
}

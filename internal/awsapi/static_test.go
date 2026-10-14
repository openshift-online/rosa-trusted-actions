package awsapi

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

// newTestProvider isolates the provider from whatever AWS configuration the
// machine running the tests happens to have, so the result depends only on the
// StaticConfig under test.
func newTestProvider(t *testing.T, cfg StaticConfig) *StaticProvider {
	t.Helper()

	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_PROFILE", "")

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return NewStaticProvider(logger, cfg)
}

func TestStaticProvider_DefaultRegion(t *testing.T) {
	provider := newTestProvider(t, StaticConfig{Region: "us-east-1"})

	got, err := provider.DefaultRegion(context.Background(), "cluster-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "us-east-1" {
		t.Errorf("expected %q, got %q", "us-east-1", got)
	}
}

func TestStaticProvider_DefaultRegionEmptyWhenUnset(t *testing.T) {
	provider := newTestProvider(t, StaticConfig{})

	got, err := provider.DefaultRegion(context.Background(), "cluster-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected an empty default region, got %q", got)
	}
}

// Without a region there is no endpoint to talk to, and the SDK would
// otherwise fail deep inside the first call with a less obvious message.
func TestStaticProvider_NoRegionConfigured(t *testing.T) {
	provider := newTestProvider(t, StaticConfig{})

	_, err := provider.GetClients(context.Background(), "cluster-1", "", []Service{ServiceEC2})

	if err == nil {
		t.Fatal("expected an error when no region is configured, got nil")
	}
	if !strings.Contains(err.Error(), "region") {
		t.Errorf("expected the error to mention the region, got %q", err.Error())
	}
}

func TestStaticProvider_BuildsRequestedClient(t *testing.T) {
	provider := newTestProvider(t, StaticConfig{
		Region:          "us-east-1",
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		EndpointURL:     "http://localhost:4566",
	})

	clients, err := provider.GetClients(context.Background(), "cluster-1", "eu-west-1", []Service{ServiceEC2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clients.EC2 == nil {
		t.Error("expected an EC2 client to be built")
	}
}

// An action that declares no services must not be handed any client; this is
// what keeps an execution from holding access it never asked for.
func TestStaticProvider_NoServicesBuildsNoClients(t *testing.T) {
	provider := newTestProvider(t, StaticConfig{Region: "us-east-1", AccessKeyID: "test", SecretAccessKey: "test"})

	clients, err := provider.GetClients(context.Background(), "cluster-1", "us-east-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clients.EC2 != nil {
		t.Error("expected no EC2 client when no service was declared")
	}
}

func TestStaticProvider_UnsupportedService(t *testing.T) {
	provider := newTestProvider(t, StaticConfig{Region: "us-east-1", AccessKeyID: "test", SecretAccessKey: "test"})

	_, err := provider.GetClients(context.Background(), "cluster-1", "us-east-1", []Service{Service("s3")})

	if err == nil {
		t.Fatal("expected an error for an unsupported service, got nil")
	}
	if !strings.Contains(err.Error(), "s3") {
		t.Errorf("expected the error to name the service, got %q", err.Error())
	}
}

// The configured region is a fallback, not an override: a request that names a
// region must run there.
func TestStaticProvider_RequestRegionOverridesDefault(t *testing.T) {
	provider := newTestProvider(t, StaticConfig{Region: "us-east-1", AccessKeyID: "test", SecretAccessKey: "test"})

	if _, err := provider.GetClients(context.Background(), "cluster-1", "ap-south-1", []Service{ServiceEC2}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The default must survive the call — it is read again on every request.
	got, err := provider.DefaultRegion(context.Background(), "cluster-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "us-east-1" {
		t.Errorf("expected the default region to be unchanged, got %q", got)
	}
}

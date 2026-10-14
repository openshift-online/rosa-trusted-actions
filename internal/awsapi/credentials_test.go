package awsapi

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	cmv1 "github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1"
	bpcredentials "github.com/openshift/backplane-cli/pkg/credentials"
	"github.com/sirupsen/logrus"
)

type fakeClusterResolver struct {
	cluster *cmv1.Cluster
	err     error

	calls  int
	lastID string
}

func (f *fakeClusterResolver) Get(_ context.Context, identifier string) (*cmv1.Cluster, error) {
	f.calls++
	f.lastID = identifier
	if f.err != nil {
		return nil, f.err
	}
	return f.cluster, nil
}

func testCluster(t *testing.T, id, cloudProvider, region string) *cmv1.Cluster {
	t.Helper()

	builder := cmv1.NewCluster().ID(id)
	if cloudProvider != "" {
		builder = builder.CloudProvider(cmv1.NewCloudProvider().ID(cloudProvider))
	}
	if region != "" {
		builder = builder.Region(cmv1.NewCloudRegion().ID(region))
	}

	cluster, err := builder.Build()
	if err != nil {
		t.Fatalf("failed to build a test cluster: %v", err)
	}
	return cluster
}

// newTestCloudCredentialsProvider builds a provider with the backplane call replaced.
// The real one needs a live backplane, OCM and STS, and unlike AWS itself
// there is no emulator for that chain — so everything this package can check
// about the provider is checked around that seam.
func newTestCloudCredentialsProvider(t *testing.T, cfg CloudCredentialsConfig, resolver ClusterResolver,
	credentials func(context.Context, *cmv1.Cluster) (*bpcredentials.AWSCredentialsResponse, error),
) *CloudCredentialsProvider {
	t.Helper()

	// Isolated from whatever AWS configuration the host has, so a client's
	// region reflects the provider's decision and nothing else.
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_PROFILE", "")

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	provider := NewCloudCredentialsProvider(logger, cfg, resolver, nil)
	if credentials != nil {
		provider.credentials = credentials
	}
	return provider
}

func staticCredentials(region string) func(context.Context, *cmv1.Cluster) (*bpcredentials.AWSCredentialsResponse, error) {
	return func(context.Context, *cmv1.Cluster) (*bpcredentials.AWSCredentialsResponse, error) {
		return &bpcredentials.AWSCredentialsResponse{
			AccessKeyID:     "AKIAEXAMPLE",
			SecretAccessKey: "secret",
			SessionToken:    "token",
			Region:          region,
		}, nil
	}
}

// The whole point of this provider: the region a request defaults to is the
// target cluster's own, not one setting shared by the fleet.
func TestCloudCredentialsProvider_DefaultRegionIsTheClustersRegion(t *testing.T) {
	resolver := &fakeClusterResolver{cluster: testCluster(t, "abc", "aws", "eu-west-2")}
	provider := newTestCloudCredentialsProvider(t, CloudCredentialsConfig{}, resolver, nil)

	region, err := provider.DefaultRegion(context.Background(), "abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if region != "eu-west-2" {
		t.Errorf("expected %q, got %q", "eu-west-2", region)
	}
	if resolver.lastID != "abc" {
		t.Errorf("expected the cluster from the request to be resolved, got %q", resolver.lastID)
	}
}

// Reported rather than returned empty: the region allowlist refuses an empty
// region, so returning one would surface as a confusing authorization denial
// instead of the missing OCM data it actually is.
func TestCloudCredentialsProvider_DefaultRegionWithoutOne(t *testing.T) {
	resolver := &fakeClusterResolver{cluster: testCluster(t, "abc", "aws", "")}
	provider := newTestCloudCredentialsProvider(t, CloudCredentialsConfig{}, resolver, nil)

	if _, err := provider.DefaultRegion(context.Background(), "abc"); err == nil {
		t.Fatal("expected an error for a cluster with no region, got nil")
	} else if !strings.Contains(err.Error(), "no region") {
		t.Errorf("expected the error to name the missing region, got %q", err.Error())
	}
}

func TestCloudCredentialsProvider_DefaultRegionLookupFailure(t *testing.T) {
	resolver := &fakeClusterResolver{err: errors.New("no cluster found")}
	provider := newTestCloudCredentialsProvider(t, CloudCredentialsConfig{}, resolver, nil)

	if _, err := provider.DefaultRegion(context.Background(), "nope"); err == nil {
		t.Fatal("expected the lookup failure to be reported, got nil")
	}
}

// Every execution needs the cluster twice — once to default the region, once
// to fetch credentials — so without the cache each AWS request would cost two
// OCM round trips.
func TestCloudCredentialsProvider_ClusterLookupIsCached(t *testing.T) {
	resolver := &fakeClusterResolver{cluster: testCluster(t, "abc", "aws", "us-east-1")}
	provider := newTestCloudCredentialsProvider(t, CloudCredentialsConfig{}, resolver, staticCredentials("us-east-1"))

	if _, err := provider.DefaultRegion(context.Background(), "abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := provider.GetClients(context.Background(), "abc", "", []Service{ServiceEC2}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolver.calls != 1 {
		t.Errorf("expected one OCM lookup across both calls, got %d", resolver.calls)
	}
}

func TestCloudCredentialsProvider_ClusterCacheExpires(t *testing.T) {
	resolver := &fakeClusterResolver{cluster: testCluster(t, "abc", "aws", "us-east-1")}
	provider := newTestCloudCredentialsProvider(t, CloudCredentialsConfig{ClusterCacheTTL: time.Minute}, resolver, nil)

	now := time.Now()
	provider.now = func() time.Time { return now }

	if _, err := provider.DefaultRegion(context.Background(), "abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := provider.DefaultRegion(context.Background(), "abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolver.calls != 2 {
		t.Errorf("expected the entry to expire and be refetched, got %d lookups", resolver.calls)
	}
}

// Caching a failure would keep a cluster unreachable for the whole TTL after a
// transient OCM error.
func TestCloudCredentialsProvider_FailedLookupIsNotCached(t *testing.T) {
	resolver := &fakeClusterResolver{err: errors.New("transient")}
	provider := newTestCloudCredentialsProvider(t, CloudCredentialsConfig{}, resolver, nil)

	for i := 0; i < 3; i++ {
		if _, err := provider.DefaultRegion(context.Background(), "abc"); err == nil {
			t.Fatal("expected an error")
		}
	}
	if resolver.calls != 3 {
		t.Errorf("expected every call to retry the lookup, got %d", resolver.calls)
	}
}

// The provider is shared by every worker in the pool, so the cluster cache is
// touched concurrently. Meaningful only under -race, which is how the suite
// runs in CI.
func TestCloudCredentialsProvider_ConcurrentUse(t *testing.T) {
	resolver := &concurrentClusterResolver{cluster: testCluster(t, "abc", "aws", "us-east-1")}
	provider := newTestCloudCredentialsProvider(t, CloudCredentialsConfig{}, resolver, staticCredentials("us-east-1"))

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := provider.DefaultRegion(context.Background(), "abc"); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if _, err := provider.GetClients(context.Background(), "abc", "", []Service{ServiceEC2}); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
}

// concurrentClusterResolver is fakeClusterResolver without the call counting,
// which would itself be the race this test is looking for.
type concurrentClusterResolver struct {
	cluster *cmv1.Cluster
}

func (r *concurrentClusterResolver) Get(context.Context, string) (*cmv1.Cluster, error) {
	return r.cluster, nil
}

// Credentials are short-lived and re-fetched every time, which is what makes
// revoking a cluster's access grant take effect on the next request.
func TestCloudCredentialsProvider_CredentialsAreNotCached(t *testing.T) {
	resolver := &fakeClusterResolver{cluster: testCluster(t, "abc", "aws", "us-east-1")}
	fetches := 0
	provider := newTestCloudCredentialsProvider(t, CloudCredentialsConfig{}, resolver,
		func(ctx context.Context, cluster *cmv1.Cluster) (*bpcredentials.AWSCredentialsResponse, error) {
			fetches++
			return staticCredentials("us-east-1")(ctx, cluster)
		})

	for i := 0; i < 3; i++ {
		if _, err := provider.GetClients(context.Background(), "abc", "", []Service{ServiceEC2}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if fetches != 3 {
		t.Errorf("expected credentials to be fetched per execution, got %d fetches", fetches)
	}
}

func TestCloudCredentialsProvider_RegionSelection(t *testing.T) {
	tests := []struct {
		name            string
		requested       string
		credentials     string
		want            string
		wantErrContains string
	}{
		{
			name:        "defaults to the region the credentials came back with",
			credentials: "us-east-1",
			want:        "us-east-1",
		},
		{
			// The credential chain always reports the cluster's own region,
			// but the account may hold resources elsewhere and the allowlist,
			// not this provider, decides which regions may be read.
			name:        "an explicit region wins",
			requested:   "ap-south-1",
			credentials: "us-east-1",
			want:        "ap-south-1",
		},
		{
			name:            "no region anywhere is an error, not a silent default",
			wantErrContains: "no AWS region",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &fakeClusterResolver{cluster: testCluster(t, "abc", "aws", "us-east-1")}
			provider := newTestCloudCredentialsProvider(t, CloudCredentialsConfig{}, resolver, staticCredentials(tc.credentials))

			clients, err := provider.GetClients(context.Background(), "abc", tc.requested, []Service{ServiceEC2})
			if tc.wantErrContains != "" {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Errorf("expected the error to mention %q, got %q", tc.wantErrContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			client, ok := clients.EC2.(*ec2.Client)
			if !ok {
				t.Fatalf("expected a real EC2 client, got %T", clients.EC2)
			}
			if got := client.Options().Region; got != tc.want {
				t.Errorf("expected the client to be built for %q, got %q", tc.want, got)
			}
		})
	}
}

// The credential chain is AWS-specific. Without this check a GCP cluster would
// fail further in, with a message about an unexpected credentials type rather
// than the actual reason.
func TestCloudCredentialsProvider_NonAWSClusterIsRejected(t *testing.T) {
	resolver := &fakeClusterResolver{cluster: testCluster(t, "abc", "gcp", "us-east1")}
	// The real fetchCredentials, because it is the thing under test here.
	provider := newTestCloudCredentialsProvider(t, CloudCredentialsConfig{}, resolver, nil)

	_, err := provider.GetClients(context.Background(), "abc", "us-east1", []Service{ServiceEC2})
	if err == nil {
		t.Fatal("expected a non-AWS cluster to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "not AWS") {
		t.Errorf("expected the error to say the cluster is not on AWS, got %q", err.Error())
	}
}

// A credential failure is the most likely failure in production — an expired
// grant, an untrusted egress IP — so it has to name the cluster it was for.
func TestCloudCredentialsProvider_CredentialFailureNamesTheCluster(t *testing.T) {
	resolver := &fakeClusterResolver{cluster: testCluster(t, "abc", "aws", "us-east-1")}
	cause := errors.New("egress IP is not trusted")
	provider := newTestCloudCredentialsProvider(t, CloudCredentialsConfig{}, resolver,
		func(context.Context, *cmv1.Cluster) (*bpcredentials.AWSCredentialsResponse, error) {
			return nil, cause
		})

	_, err := provider.GetClients(context.Background(), "abc", "us-east-1", []Service{ServiceEC2})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "abc") {
		t.Errorf("expected the error to name the cluster, got %q", err.Error())
	}
	if !errors.Is(err, cause) {
		t.Errorf("expected the underlying cause to be wrapped, got %q", err.Error())
	}
}

func TestCloudCredentialsProvider_UnsupportedService(t *testing.T) {
	resolver := &fakeClusterResolver{cluster: testCluster(t, "abc", "aws", "us-east-1")}
	provider := newTestCloudCredentialsProvider(t, CloudCredentialsConfig{}, resolver, staticCredentials("us-east-1"))

	_, err := provider.GetClients(context.Background(), "abc", "us-east-1", []Service{Service("s3")})
	if err == nil {
		t.Fatal("expected an unsupported service to be rejected, got nil")
	}
}

func TestCloudCredentialsProvider_InvalidProxyURL(t *testing.T) {
	resolver := &fakeClusterResolver{cluster: testCluster(t, "abc", "aws", "us-east-1")}
	provider := newTestCloudCredentialsProvider(t, CloudCredentialsConfig{AWSProxyURL: "://nope"}, resolver, staticCredentials("us-east-1"))

	_, err := provider.GetClients(context.Background(), "abc", "us-east-1", []Service{ServiceEC2})
	if err == nil {
		t.Fatal("expected an unparseable proxy URL to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "proxy") {
		t.Errorf("expected the error to name the proxy, got %q", err.Error())
	}
}

// Matches backplane-cli's own GetAwsProxy: one proxy setting covers both
// unless AWS traffic needs a different one.
func TestCloudCredentialsProvider_AWSProxyFallsBackToTheBackplaneProxy(t *testing.T) {
	tests := []struct {
		name      string
		config    CloudCredentialsConfig
		wantProxy string
	}{
		{name: "neither set", config: CloudCredentialsConfig{}, wantProxy: ""},
		{name: "backplane only", config: CloudCredentialsConfig{ProxyURL: "http://bp:3128"}, wantProxy: "http://bp:3128"},
		{name: "aws only", config: CloudCredentialsConfig{AWSProxyURL: "http://aws:3128"}, wantProxy: "http://aws:3128"},
		{
			name:      "aws overrides backplane",
			config:    CloudCredentialsConfig{ProxyURL: "http://bp:3128", AWSProxyURL: "http://aws:3128"},
			wantProxy: "http://aws:3128",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			provider := newTestCloudCredentialsProvider(t, tc.config, &fakeClusterResolver{}, nil)
			if got := provider.awsProxy(); got != tc.wantProxy {
				t.Errorf("expected %q, got %q", tc.wantProxy, got)
			}
		})
	}
}

// backplane-cli's configuration distinguishes "not configured" from "empty" by
// nil-ness, so an unset setting must not arrive as a pointer to "".
func TestOptionalString(t *testing.T) {
	if got := optionalString(""); got != nil {
		t.Errorf("expected nil for an unset value, got %q", *got)
	}
	got := optionalString("http://proxy:3128")
	if got == nil || *got != "http://proxy:3128" {
		t.Errorf("expected the value to be carried through, got %v", got)
	}
}

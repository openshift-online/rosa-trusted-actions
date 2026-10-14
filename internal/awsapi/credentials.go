package awsapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	ocmsdk "github.com/openshift-online/ocm-sdk-go"
	cmv1 "github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1"
	bpcloud "github.com/openshift/backplane-cli/cmd/ocm-backplane/cloud"
	bpconfig "github.com/openshift/backplane-cli/pkg/cli/config"
	bpcredentials "github.com/openshift/backplane-cli/pkg/credentials"
	"github.com/sirupsen/logrus"
)

// ClusterResolver turns the cluster identifier a request carries into the OCM
// cluster record. It is declared here as a narrow interface, like the AWS
// service interfaces above, so the provider can be exercised without a live
// OCM connection. internal/ocm.Clusters satisfies it.
type ClusterResolver interface {
	Get(ctx context.Context, identifier string) (*cmv1.Cluster, error)
}

// defaultClusterCacheTTL bounds how stale a cached cluster record may be. Only
// the cluster's region and cloud provider are read from it, neither of which
// changes over a cluster's life, so this is short mainly to bound the effect of
// a cluster being deleted.
const defaultClusterCacheTTL = 5 * time.Minute

// CloudCredentialsConfig configures the backplane credential flow.
type CloudCredentialsConfig struct {
	// URL is the backplane API base URL.
	URL string
	// AssumeInitialARN is the payer-account role the assume-role chain starts
	// from. It is optional: backplane-cli derives it from the OCM URL for the
	// production, staging and integration environments, and rejects any value
	// that is not one of those three roles.
	AssumeInitialARN string
	// ProxyURL is the proxy used to reach the backplane API, AWSProxyURL the
	// one used to reach AWS. Both are optional; when AWSProxyURL is empty,
	// AWS traffic falls back to ProxyURL, matching backplane-cli.
	ProxyURL    string
	AWSProxyURL string
	// ClusterCacheTTL is how long a resolved cluster record is reused before
	// OCM is asked again. Zero selects defaultClusterCacheTTL.
	ClusterCacheTTL time.Duration
}

type cachedCluster struct {
	cluster *cmv1.Cluster
	expires time.Time
}

// CloudCredentialsProvider builds AWS clients from short-lived credentials for
// the target cluster's own AWS account. It is the production counterpart of
// StaticProvider, which reaches a single account for the whole fleet.
//
// Named for what it obtains rather than for the service it obtains it from,
// because internal/backplane.BackplaneProvider is a different thing that talks
// to the same service: that one calls the HMAC-signed trusted-action endpoint
// and comes back with a kubeconfig, this one runs backplane-cli's
// cloud-credential flow and comes back with AWS credentials. Both appear in
// cmd/server/main.go, so sharing a name made one read as the other.
//
// It mirrors what configuration-anomaly-detection does in pkg/managedcloud:
// hand the cluster and an OCM connection to backplane-cli, which walks the SRE
// assume-role chain from the payer account into the customer's account and
// returns temporary STS credentials scoped to it. Two consequences matter when
// deploying this:
//
//   - The chain only grants credentials when the caller's egress IP is inside
//     the trusted range OCM publishes, so the server must run from a trusted
//     network, or reach AWS through AWSProxyURL.
//   - Credentials are fetched per execution rather than cached, so revoking a
//     cluster's access grant takes effect on the next request.
type CloudCredentialsProvider struct {
	logger     *logrus.Logger
	config     CloudCredentialsConfig
	clusters   ClusterResolver
	connection *ocmsdk.Connection

	// credentials is the call into backplane-cli. It is a field so tests can
	// replace it: the real one needs a live backplane, OCM and STS, and there
	// is no emulator for that chain the way LocalStack emulates AWS itself.
	credentials func(ctx context.Context, cluster *cmv1.Cluster) (*bpcredentials.AWSCredentialsResponse, error)

	now          func() time.Time
	mu           sync.Mutex
	clusterCache map[string]cachedCluster
}

var _ ClientProvider = (*CloudCredentialsProvider)(nil)

func NewCloudCredentialsProvider(logger *logrus.Logger, cfg CloudCredentialsConfig, clusters ClusterResolver, connection *ocmsdk.Connection) *CloudCredentialsProvider {
	if cfg.ClusterCacheTTL == 0 {
		cfg.ClusterCacheTTL = defaultClusterCacheTTL
	}
	provider := &CloudCredentialsProvider{
		logger:       logger,
		config:       cfg,
		clusters:     clusters,
		connection:   connection,
		now:          time.Now,
		clusterCache: map[string]cachedCluster{},
	}
	provider.credentials = provider.fetchCredentials
	return provider
}

// DefaultRegion is the target cluster's own region. A cluster's AWS resources
// live where the cluster does, so this is the only sensible default — and
// because the region allowlist refuses a request with no region at all, it is
// what makes omitting the param work in production.
func (p *CloudCredentialsProvider) DefaultRegion(ctx context.Context, clusterID string) (string, error) {
	cluster, err := p.cluster(ctx, clusterID)
	if err != nil {
		return "", err
	}
	region := cluster.Region().ID()
	if region == "" {
		return "", fmt.Errorf("cluster %q has no region recorded in OCM", clusterID)
	}
	return region, nil
}

// ClusterInfraID is the cluster's infrastructure ID as OCM records it. It
// reads the same cached cluster object as DefaultRegion, so the ownership
// check costs no extra OCM call.
//
// A missing infra ID is an error rather than an empty answer, mirroring the
// region above: against real OCM, a cluster without one is an anomaly, and
// treating it as "no cluster owns anything here" would be a worse answer than
// saying so.
func (p *CloudCredentialsProvider) ClusterInfraID(ctx context.Context, clusterID string) (string, error) {
	cluster, err := p.cluster(ctx, clusterID)
	if err != nil {
		return "", err
	}
	infraID := cluster.InfraID()
	if infraID == "" {
		return "", fmt.Errorf("cluster %q has no infrastructure ID recorded in OCM", clusterID)
	}
	return infraID, nil
}

func (p *CloudCredentialsProvider) GetClients(ctx context.Context, clusterID, region string, services []Service) (Clients, error) {
	cluster, err := p.cluster(ctx, clusterID)
	if err != nil {
		return Clients{}, err
	}

	awsCredentials, err := p.credentials(ctx, cluster)
	if err != nil {
		return Clients{}, fmt.Errorf("failed to obtain AWS credentials for cluster %q from backplane: %w", clusterID, err)
	}

	// Both sources are the same cluster's region in practice — the scope
	// resolves it via DefaultRegion below, and the credential chain reports
	// cluster.Region().ID() too — so this is a fallback for a caller that
	// passed none, not a precedence rule between two different answers.
	if region == "" {
		region = awsCredentials.Region
	}
	if region == "" {
		return Clients{}, fmt.Errorf("no AWS region for cluster %q", clusterID)
	}

	p.logger.WithFields(logrus.Fields{
		"cluster_id": clusterID,
		"region":     region,
		"services":   services,
	}).Debug("building AWS clients from backplane credentials")

	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			awsCredentials.AccessKeyID,
			awsCredentials.SecretAccessKey,
			awsCredentials.SessionToken,
		)),
	}
	if proxy := p.awsProxy(); proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return Clients{}, fmt.Errorf("invalid AWS proxy URL %q: %w", proxy, err)
		}
		options = append(options, awsconfig.WithHTTPClient(&http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		}))
	}

	awsConfig, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return Clients{}, fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	return buildClients(awsConfig, services)
}

// fetchCredentials runs backplane-cli's cloud credential flow for one cluster.
func (p *CloudCredentialsProvider) fetchCredentials(_ context.Context, cluster *cmv1.Cluster) (*bpcredentials.AWSCredentialsResponse, error) {
	// Checked here rather than left to the credential call, which does not
	// look: on a non-AWS cluster it would return a credentials type for the
	// other cloud and the assertion below would fail with a confusing message.
	if id := cluster.CloudProvider().ID(); id != "aws" {
		return nil, fmt.Errorf("cluster %q runs on %q, not AWS", cluster.ID(), id)
	}

	query := &bpcloud.QueryConfig{
		OcmConnection: p.connection,
		Cluster:       cluster,
		BackplaneConfiguration: bpconfig.BackplaneConfiguration{
			URL:              p.config.URL,
			AssumeInitialArn: p.config.AssumeInitialARN,
			ProxyURL:         optionalString(p.config.ProxyURL),
			AwsProxy:         optionalString(p.config.AWSProxyURL),
		},
	}

	// GetCloudCredentials rather than the GetAWSV2Config that CAD calls. Both
	// run this same chain — GetAWSV2Config is a thin wrapper over this call —
	// but it then loads backplane-cli's own config file from disk to assemble
	// the aws.Config, and pins the region to the cluster's. A long-running
	// server has no such file and needs the requested region honoured, so the
	// aws.Config is assembled in GetClients instead.
	//
	// The context is not propagated because backplane-cli's flow takes none.
	response, err := query.GetCloudCredentials()
	if err != nil {
		return nil, err
	}

	awsCredentials, ok := response.(*bpcredentials.AWSCredentialsResponse)
	if !ok {
		return nil, fmt.Errorf("backplane returned %T for cluster %q, not AWS credentials", response, cluster.ID())
	}
	return awsCredentials, nil
}

// cluster resolves a cluster identifier, reusing a recent answer.
//
// The cache is here because every execution needs the cluster twice — once to
// default the region before authorization, once to fetch credentials — and the
// fields read from it (region, cloud provider) do not change over a cluster's
// life. Credentials are deliberately not cached: they are short-lived, and
// re-fetching them per execution is what makes a revoked grant take effect.
//
// The map is never pruned, so it grows to the number of distinct clusters the
// server serves; entries are a few KB each and a failed lookup is not stored.
func (p *CloudCredentialsProvider) cluster(ctx context.Context, clusterID string) (*cmv1.Cluster, error) {
	p.mu.Lock()
	entry, cached := p.clusterCache[clusterID]
	p.mu.Unlock()
	if cached && p.now().Before(entry.expires) {
		return entry.cluster, nil
	}

	// Deliberately not holding the lock across the lookup: two requests for
	// the same cluster may both fetch, which costs a duplicate read of
	// immutable data, whereas holding it would queue every AWS execution in
	// the server behind one slow OCM call.
	cluster, err := p.clusters.Get(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.clusterCache[clusterID] = cachedCluster{cluster: cluster, expires: p.now().Add(p.config.ClusterCacheTTL)}
	p.mu.Unlock()

	return cluster, nil
}

// awsProxy is the proxy AWS traffic goes through, falling back to the
// backplane proxy the way backplane-cli's own GetAwsProxy does.
func (p *CloudCredentialsProvider) awsProxy() string {
	if p.config.AWSProxyURL != "" {
		return p.config.AWSProxyURL
	}
	return p.config.ProxyURL
}

// optionalString maps an unset setting to a nil pointer, which is how
// backplane-cli's configuration distinguishes "not configured" from "empty".
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

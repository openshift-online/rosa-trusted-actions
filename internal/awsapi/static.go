package awsapi

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/sirupsen/logrus"
)

// StaticConfig is the credential and endpoint configuration a StaticProvider
// builds its clients from. Empty fields fall back to the SDK's default
// resolution chain (environment, shared config, instance metadata).
type StaticConfig struct {
	// Region is the default region used when a request does not name one.
	Region string
	// AccessKeyID, SecretAccessKey and SessionToken, when key and secret are
	// both set, replace the SDK's default credential chain.
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	// EndpointURL overrides the AWS service endpoint. It exists to point the
	// provider at a local AWS emulator (LocalStack) for testing.
	EndpointURL string
	// InfraID is the cluster infrastructure ID write actions check instance
	// ownership against. It is configured here for the same reason Region is:
	// this provider has no OCM to ask, so the one answer it has must be given
	// to it. Left empty, ownership cannot be proven and every write is refused.
	InfraID string
}

// StaticProvider builds AWS clients from one fixed set of credentials.
//
// It is the AWS counterpart of backplane.KubeconfigProvider: a single
// preconfigured identity for local and test environments. In particular it
// ignores clusterID — resolving a cluster to its own AWS account (assuming a
// per-account role via STS) is not implemented, so every execution reaches the
// same account regardless of which cluster it names. Production deployments
// must not rely on this provider for per-cluster isolation.
type StaticProvider struct {
	logger *logrus.Logger
	config StaticConfig
}

var _ ClientProvider = (*StaticProvider)(nil)

func NewStaticProvider(logger *logrus.Logger, cfg StaticConfig) *StaticProvider {
	return &StaticProvider{logger: logger, config: cfg}
}

// DefaultRegion ignores the cluster: this provider has one account and one
// configured region for the whole fleet, which is exactly the limitation that
// keeps it out of production.
func (p *StaticProvider) DefaultRegion(_ context.Context, _ string) (string, error) {
	return p.config.Region, nil
}

// ClusterInfraID ignores the cluster for the same reason DefaultRegion does:
// there is no OCM here to look one up in, only the single value configured.
// Unset, it returns empty and write actions refuse — which is the right
// outcome, since this provider cannot tell one cluster's instances from
// another's anyway.
func (p *StaticProvider) ClusterInfraID(_ context.Context, _ string) (string, error) {
	return p.config.InfraID, nil
}

func (p *StaticProvider) GetClients(ctx context.Context, clusterID, region string, services []Service) (Clients, error) {
	if region == "" {
		region = p.config.Region
	}
	if region == "" {
		return Clients{}, fmt.Errorf("no AWS region configured: set AWS_REGION or pass a region")
	}

	p.logger.WithFields(logrus.Fields{
		"cluster_id": clusterID,
		"region":     region,
		"services":   services,
	}).Debug("building AWS clients from static configuration")

	awsConfig, err := p.load(ctx, region)
	if err != nil {
		return Clients{}, fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	return buildClients(awsConfig, services)
}

func (p *StaticProvider) load(ctx context.Context, region string) (aws.Config, error) {
	options := []func(*config.LoadOptions) error{config.WithRegion(region)}

	if p.config.AccessKeyID != "" && p.config.SecretAccessKey != "" {
		options = append(options, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				p.config.AccessKeyID,
				p.config.SecretAccessKey,
				p.config.SessionToken,
			),
		))
	}

	if p.config.EndpointURL != "" {
		options = append(options, config.WithBaseEndpoint(p.config.EndpointURL))
	}

	return config.LoadDefaultConfig(ctx, options...)
}

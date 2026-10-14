// Package awsapi provisions the AWS service clients an action runs against.
// It is the AWS counterpart of internal/backplane: the executor asks a
// ClientProvider for the clients an action declared it needs, and the provider
// decides how credentials for the target cluster's account are obtained.
package awsapi

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/route53"
)

// Service identifies one AWS service an action needs a client for. Actions
// declare their services so a provider only builds what is actually used,
// rather than constructing every supported client on every execution.
type Service string

const (
	ServiceEC2        Service = "ec2"
	ServiceELB        Service = "elb"
	ServiceELBv2      Service = "elbv2"
	ServiceCloudTrail Service = "cloudtrail"
	ServiceRoute53    Service = "route53"
)

// EC2API is the subset of the EC2 client the actions use. Narrowing it here
// keeps actions testable against a fake instead of a live account.
type EC2API interface {
	DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	DescribeSecurityGroups(ctx context.Context, in *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	DescribeSubnets(ctx context.Context, in *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
	DescribeRouteTables(ctx context.Context, in *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error)
	DescribeVpcs(ctx context.Context, in *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
	DescribeDhcpOptions(ctx context.Context, in *ec2.DescribeDhcpOptionsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeDhcpOptionsOutput, error)

	// The only mutating calls in this package. DescribeInstances above is also
	// what the write actions use to check an instance belongs to the cluster
	// before calling either of these, so a write needs no service the reads
	// don't already have.
	RebootInstances(ctx context.Context, in *ec2.RebootInstancesInput, optFns ...func(*ec2.Options)) (*ec2.RebootInstancesOutput, error)
	TerminateInstances(ctx context.Context, in *ec2.TerminateInstancesInput, optFns ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
}

// ELBv2API is the subset of the Elastic Load Balancing v2 client the actions
// use. v2 covers application, network and gateway load balancers.
type ELBv2API interface {
	DescribeLoadBalancers(ctx context.Context, in *elbv2.DescribeLoadBalancersInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error)
	DescribeTargetGroups(ctx context.Context, in *elbv2.DescribeTargetGroupsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTargetGroupsOutput, error)
	DescribeTargetHealth(ctx context.Context, in *elbv2.DescribeTargetHealthInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTargetHealthOutput, error)
}

// ELBAPI is the subset of the Elastic Load Balancing v1 client the actions use.
// v1 is a separate API, not an older version of the same one: classic load
// balancers are invisible to ELBv2 and vice versa, so a cluster's networking
// can only be read in full by querying both.
type ELBAPI interface {
	DescribeLoadBalancers(ctx context.Context, in *elb.DescribeLoadBalancersInput, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error)
}

// CloudTrailAPI is the subset of the CloudTrail client the actions use.
type CloudTrailAPI interface {
	LookupEvents(ctx context.Context, in *cloudtrail.LookupEventsInput, optFns ...func(*cloudtrail.Options)) (*cloudtrail.LookupEventsOutput, error)
}

// Route53API is the subset of the Route 53 client the actions use.
type Route53API interface {
	ListHostedZones(ctx context.Context, in *route53.ListHostedZonesInput, optFns ...func(*route53.Options)) (*route53.ListHostedZonesOutput, error)
	ListResourceRecordSets(ctx context.Context, in *route53.ListResourceRecordSetsInput, optFns ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error)
}

// Clients carries the service clients provisioned for one execution. A field
// is nil when the action did not declare the corresponding Service.
type Clients struct {
	EC2        EC2API
	ELB        ELBAPI
	ELBv2      ELBv2API
	CloudTrail CloudTrailAPI
	Route53    Route53API
}

// ClientProvider resolves AWS credentials for a cluster's account and builds
// the requested service clients from them.
type ClientProvider interface {
	// DefaultRegion is the region every request against this cluster reads.
	// It is not a fallback: callers never name a region, so this is the only
	// answer. Callers resolve it before authorizing, so the region a request
	// is judged and audited against is the one it actually runs in.
	//
	// It takes the cluster because the answer depends on it: a cluster's AWS
	// resources live in that cluster's own region, and a provider that reaches
	// real AWS has to look that up rather than assume one region for the whole
	// fleet. It returns an empty string, without an error, when the provider
	// simply has none configured.
	DefaultRegion(ctx context.Context, clusterID string) (string, error)

	// ClusterInfraID is the cluster's infrastructure ID, the name its AWS
	// resources are tagged with. It is how a write action tells the cluster's
	// own instances apart from everything else sharing the account, so it is
	// resolved from the same authoritative source as the region — never from
	// anything the caller sent.
	//
	// An empty string, without an error, means the provider cannot answer.
	// Callers must treat that as "ownership unproven" and refuse to write,
	// because an unanswered ownership question is not a passed one.
	ClusterInfraID(ctx context.Context, clusterID string) (string, error)

	// GetClients builds the requested service clients against the target
	// cluster's AWS account. An empty region means the provider's own.
	GetClients(ctx context.Context, clusterID, region string, services []Service) (Clients, error)
}

// buildClients constructs the requested service clients from one AWS config.
// Every provider goes through here, so adding a Service cannot leave one of
// them silently handing an action a nil client — the failure would otherwise
// only appear when the read dereferenced it.
func buildClients(cfg aws.Config, services []Service) (Clients, error) {
	clients := Clients{}
	for _, service := range services {
		switch service {
		case ServiceEC2:
			clients.EC2 = ec2.NewFromConfig(cfg)
		case ServiceELB:
			clients.ELB = elb.NewFromConfig(cfg)
		case ServiceELBv2:
			clients.ELBv2 = elbv2.NewFromConfig(cfg)
		case ServiceCloudTrail:
			clients.CloudTrail = cloudtrail.NewFromConfig(cfg)
		case ServiceRoute53:
			clients.Route53 = route53.NewFromConfig(cfg)
		default:
			return Clients{}, fmt.Errorf("unsupported AWS service %q", service)
		}
	}
	return clients, nil
}

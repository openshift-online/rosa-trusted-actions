package actions

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

// arnOrName splits a caller-supplied identifier into the two input fields the
// ELBv2 APIs use. An SRE has whichever identifier is in front of them, and
// ARNs are unambiguous — they always begin with "arn:" — so the two can be
// told apart here rather than making the caller declare which they meant.
func arnOrName(id string) (arns, names []string) {
	if strings.HasPrefix(id, "arn:") {
		return []string{id}, nil
	}
	return nil, []string{id}
}

// describeLoadBalancers reads application, network and gateway load balancers.
// Classic load balancers are a separate API and are invisible here; see
// describeClassicLoadBalancers.
func describeLoadBalancers(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error) {
	input := &elbv2.DescribeLoadBalancersInput{}
	if req.Target.ID != "" {
		input.LoadBalancerArns, input.Names = arnOrName(req.Target.ID)
	}

	balancers := []elbv2types.LoadBalancer{}
	paginator := elbv2.NewDescribeLoadBalancersPaginator(clients.ELBv2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe load balancers in %s: %w", req.Target.Region, err)
		}
		balancers = append(balancers, page.LoadBalancers...)
	}

	return awsResourcesFrom(balancers)
}

// describeClassicLoadBalancers reads classic load balancers. ELB v1 is a
// separate API rather than an older version of v2, so a cluster's load
// balancers are only visible in full by reading both.
func describeClassicLoadBalancers(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error) {
	input := &elb.DescribeLoadBalancersInput{}
	if req.Target.ID != "" {
		// Classic load balancers have no ARN in this API — they are addressed
		// by name only.
		input.LoadBalancerNames = []string{req.Target.ID}
	}

	balancers := []elbtypes.LoadBalancerDescription{}
	paginator := elb.NewDescribeLoadBalancersPaginator(clients.ELB, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe classic load balancers in %s: %w", req.Target.Region, err)
		}
		balancers = append(balancers, page.LoadBalancerDescriptions...)
	}

	return awsResourcesFrom(balancers)
}

// describeTargetGroups reads target groups.
func describeTargetGroups(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error) {
	input := &elbv2.DescribeTargetGroupsInput{}
	if req.Target.ID != "" {
		input.TargetGroupArns, input.Names = arnOrName(req.Target.ID)
	}

	groups := []elbv2types.TargetGroup{}
	paginator := elbv2.NewDescribeTargetGroupsPaginator(clients.ELBv2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe target groups in %s: %w", req.Target.Region, err)
		}
		groups = append(groups, page.TargetGroups...)
	}

	return awsResourcesFrom(groups)
}

// describeTargetHealth reports the health of the targets registered with one
// target group. The id is that group's ARN: there is no call that reports
// target health account-wide, which is why the descriptor marks the id
// required. The API returns every target in one response, so unlike the reads
// above there is no paginator to drive.
func describeTargetHealth(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error) {
	out, err := clients.ELBv2.DescribeTargetHealth(ctx, &elbv2.DescribeTargetHealthInput{
		TargetGroupArn: aws.String(req.Target.ID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe target health for %s in %s: %w", req.Target.ID, req.Target.Region, err)
	}

	return awsResourcesFrom(out.TargetHealthDescriptions)
}

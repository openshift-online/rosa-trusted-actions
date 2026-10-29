package actions

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

// ec2Filters converts the caller's "filters" param into the SDK's filter type.
// Every EC2 describe accepts the same filter shape, so the conversion happens
// once here instead of in each read.
func ec2Filters(params map[string]string) ([]ec2types.Filter, error) {
	parsed, err := parseAWSFilters(params["filters"])
	if err != nil {
		return nil, err
	}
	if len(parsed) == 0 {
		return nil, nil
	}

	filters := make([]ec2types.Filter, 0, len(parsed))
	for _, filter := range parsed {
		filters = append(filters, ec2types.Filter{
			Name:   aws.String(filter.Name),
			Values: filter.Values,
		})
	}
	return filters, nil
}

// describeEC2Instances reads EC2 instances. DescribeInstances groups instances
// under reservations, which is an artefact of how they were launched rather
// than something an SRE is asking about, so the reservations are flattened
// away and the instances returned directly.
func describeEC2Instances(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error) {
	input := &ec2.DescribeInstancesInput{}
	if req.Target.ID != "" {
		input.InstanceIds = []string{req.Target.ID}
	}

	var err error
	if input.Filters, err = ec2Filters(req.Params); err != nil {
		return nil, err
	}

	instances := []ec2types.Instance{}
	paginator := ec2.NewDescribeInstancesPaginator(clients.EC2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe EC2 instances in %s: %w", req.Target.Region, err)
		}
		for _, reservation := range page.Reservations {
			instances = append(instances, reservation.Instances...)
		}
	}

	return awsResourcesFrom(instances)
}

// describeEC2SecurityGroups reads security groups.
func describeEC2SecurityGroups(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error) {
	input := &ec2.DescribeSecurityGroupsInput{}
	if req.Target.ID != "" {
		input.GroupIds = []string{req.Target.ID}
	}

	var err error
	if input.Filters, err = ec2Filters(req.Params); err != nil {
		return nil, err
	}

	groups := []ec2types.SecurityGroup{}
	paginator := ec2.NewDescribeSecurityGroupsPaginator(clients.EC2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe EC2 security groups in %s: %w", req.Target.Region, err)
		}
		groups = append(groups, page.SecurityGroups...)
	}

	return awsResourcesFrom(groups)
}

// describeEC2Subnets reads subnets.
func describeEC2Subnets(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error) {
	input := &ec2.DescribeSubnetsInput{}
	if req.Target.ID != "" {
		input.SubnetIds = []string{req.Target.ID}
	}

	var err error
	if input.Filters, err = ec2Filters(req.Params); err != nil {
		return nil, err
	}

	subnets := []ec2types.Subnet{}
	paginator := ec2.NewDescribeSubnetsPaginator(clients.EC2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe EC2 subnets in %s: %w", req.Target.Region, err)
		}
		subnets = append(subnets, page.Subnets...)
	}

	return awsResourcesFrom(subnets)
}

// describeEC2RouteTables reads route tables.
func describeEC2RouteTables(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error) {
	input := &ec2.DescribeRouteTablesInput{}
	if req.Target.ID != "" {
		input.RouteTableIds = []string{req.Target.ID}
	}

	var err error
	if input.Filters, err = ec2Filters(req.Params); err != nil {
		return nil, err
	}

	tables := []ec2types.RouteTable{}
	paginator := ec2.NewDescribeRouteTablesPaginator(clients.EC2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe EC2 route tables in %s: %w", req.Target.Region, err)
		}
		tables = append(tables, page.RouteTables...)
	}

	return awsResourcesFrom(tables)
}

// describeEC2Vpcs reads VPCs.
func describeEC2Vpcs(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error) {
	input := &ec2.DescribeVpcsInput{}
	if req.Target.ID != "" {
		input.VpcIds = []string{req.Target.ID}
	}

	var err error
	if input.Filters, err = ec2Filters(req.Params); err != nil {
		return nil, err
	}

	vpcs := []ec2types.Vpc{}
	paginator := ec2.NewDescribeVpcsPaginator(clients.EC2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe EC2 VPCs in %s: %w", req.Target.Region, err)
		}
		vpcs = append(vpcs, page.Vpcs...)
	}

	return awsResourcesFrom(vpcs)
}

// describeEC2DhcpOptions reads DHCP option sets, which is where a VPC's DNS
// server configuration lives.
func describeEC2DhcpOptions(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error) {
	input := &ec2.DescribeDhcpOptionsInput{}
	if req.Target.ID != "" {
		input.DhcpOptionsIds = []string{req.Target.ID}
	}

	var err error
	if input.Filters, err = ec2Filters(req.Params); err != nil {
		return nil, err
	}

	options := []ec2types.DhcpOptions{}
	paginator := ec2.NewDescribeDhcpOptionsPaginator(clients.EC2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe EC2 DHCP options in %s: %w", req.Target.Region, err)
		}
		options = append(options, page.DhcpOptions...)
	}

	return awsResourcesFrom(options)
}

package actions

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

// ec2Op is one describe operation's fixture: the pages it serves — each page a
// list of object IDs — and the inputs it was called with. Every EC2 read
// paginates the same way, so the page bookkeeping is written once.
type ec2Op[In any] struct {
	pages [][]string
	calls []*In
}

// next records the call and returns the IDs making up this page, plus the
// token that asks the paginator for another one (nil on the last page).
func (o *ec2Op[In]) next(in *In) ([]string, *string) {
	o.calls = append(o.calls, in)

	index := len(o.calls) - 1
	if index >= len(o.pages) {
		return nil, nil
	}

	var token *string
	if index+1 < len(o.pages) {
		token = aws.String(fmt.Sprintf("page-%d", index+1))
	}
	return o.pages[index], token
}

// fakeEC2API implements the whole EC2API surface. Each operation keeps its own
// pages and call log, so a test for one resource cannot pass on another's
// fixtures. A non-nil err fails every operation.
type fakeEC2API struct {
	err error

	instances      ec2Op[ec2.DescribeInstancesInput]
	securityGroups ec2Op[ec2.DescribeSecurityGroupsInput]
	subnets        ec2Op[ec2.DescribeSubnetsInput]
	routeTables    ec2Op[ec2.DescribeRouteTablesInput]
	vpcs           ec2Op[ec2.DescribeVpcsInput]
	dhcpOptions    ec2Op[ec2.DescribeDhcpOptionsInput]

	// Write-side fixtures. instanceDetails supplies whole instances rather than
	// bare IDs, because the ownership check reads tags and the id-only pages
	// above cannot express one. The two call logs let a test assert that a
	// refused write made no AWS call at all.
	instanceDetails  []ec2types.Instance
	rebooted         []*ec2.RebootInstancesInput
	terminated       []*ec2.TerminateInstancesInput
	rebootErr        error
	terminateErr     error
	terminatingState *ec2types.InstanceStateChange
}

// DescribeInstances returns each page as a single reservation, so the fake
// also exercises the reservation flattening the real API forces on us.
func (f *fakeEC2API) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	ids, token := f.instances.next(in)
	if f.err != nil {
		return nil, f.err
	}

	// One unpaginated page: the write path describes a single instance by id,
	// so there is nothing to paginate.
	if f.instanceDetails != nil {
		return &ec2.DescribeInstancesOutput{
			Reservations: []ec2types.Reservation{{Instances: f.instanceDetails}},
		}, nil
	}

	instances := make([]ec2types.Instance, 0, len(ids))
	for _, id := range ids {
		instances = append(instances, ec2types.Instance{InstanceId: aws.String(id)})
	}
	return &ec2.DescribeInstancesOutput{
		Reservations: []ec2types.Reservation{{Instances: instances}},
		NextToken:    token,
	}, nil
}

func (f *fakeEC2API) DescribeSecurityGroups(_ context.Context, in *ec2.DescribeSecurityGroupsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	ids, token := f.securityGroups.next(in)
	if f.err != nil {
		return nil, f.err
	}

	groups := make([]ec2types.SecurityGroup, 0, len(ids))
	for _, id := range ids {
		groups = append(groups, ec2types.SecurityGroup{GroupId: aws.String(id)})
	}
	return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: groups, NextToken: token}, nil
}

func (f *fakeEC2API) DescribeSubnets(_ context.Context, in *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	ids, token := f.subnets.next(in)
	if f.err != nil {
		return nil, f.err
	}

	subnets := make([]ec2types.Subnet, 0, len(ids))
	for _, id := range ids {
		subnets = append(subnets, ec2types.Subnet{SubnetId: aws.String(id)})
	}
	return &ec2.DescribeSubnetsOutput{Subnets: subnets, NextToken: token}, nil
}

func (f *fakeEC2API) DescribeRouteTables(_ context.Context, in *ec2.DescribeRouteTablesInput, _ ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
	ids, token := f.routeTables.next(in)
	if f.err != nil {
		return nil, f.err
	}

	tables := make([]ec2types.RouteTable, 0, len(ids))
	for _, id := range ids {
		tables = append(tables, ec2types.RouteTable{RouteTableId: aws.String(id)})
	}
	return &ec2.DescribeRouteTablesOutput{RouteTables: tables, NextToken: token}, nil
}

func (f *fakeEC2API) DescribeVpcs(_ context.Context, in *ec2.DescribeVpcsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	ids, token := f.vpcs.next(in)
	if f.err != nil {
		return nil, f.err
	}

	vpcs := make([]ec2types.Vpc, 0, len(ids))
	for _, id := range ids {
		vpcs = append(vpcs, ec2types.Vpc{VpcId: aws.String(id)})
	}
	return &ec2.DescribeVpcsOutput{Vpcs: vpcs, NextToken: token}, nil
}

func (f *fakeEC2API) DescribeDhcpOptions(_ context.Context, in *ec2.DescribeDhcpOptionsInput, _ ...func(*ec2.Options)) (*ec2.DescribeDhcpOptionsOutput, error) {
	ids, token := f.dhcpOptions.next(in)
	if f.err != nil {
		return nil, f.err
	}

	options := make([]ec2types.DhcpOptions, 0, len(ids))
	for _, id := range ids {
		options = append(options, ec2types.DhcpOptions{DhcpOptionsId: aws.String(id)})
	}
	return &ec2.DescribeDhcpOptionsOutput{DhcpOptions: options, NextToken: token}, nil
}

func (f *fakeEC2API) RebootInstances(_ context.Context, in *ec2.RebootInstancesInput, _ ...func(*ec2.Options)) (*ec2.RebootInstancesOutput, error) {
	f.rebooted = append(f.rebooted, in)
	if f.rebootErr != nil {
		return nil, f.rebootErr
	}
	return &ec2.RebootInstancesOutput{}, nil
}

func (f *fakeEC2API) TerminateInstances(_ context.Context, in *ec2.TerminateInstancesInput, _ ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	f.terminated = append(f.terminated, in)
	if f.terminateErr != nil {
		return nil, f.terminateErr
	}
	output := &ec2.TerminateInstancesOutput{}
	if f.terminatingState != nil {
		output.TerminatingInstances = []ec2types.InstanceStateChange{*f.terminatingState}
	}
	return output, nil
}

// resourceIDs pulls one field out of every returned object, so a test can
// assert on what came back without caring about the rest of the shape.
func resourceIDs(t *testing.T, key string, resources []map[string]interface{}) []string {
	t.Helper()
	ids := make([]string, 0, len(resources))
	for _, resource := range resources {
		id, ok := resource[key].(string)
		if !ok {
			t.Fatalf("expected a %s string, got %#v", key, resource[key])
		}
		ids = append(ids, id)
	}
	return ids
}

func instanceIDs(t *testing.T, resources []map[string]interface{}) []string {
	t.Helper()
	return resourceIDs(t, "InstanceId", resources)
}

// Instances arrive grouped under reservations and spread over pages; the
// action must return one flat list regardless.
func TestDescribeEC2Instances_FlattensPagesAndReservations(t *testing.T) {
	client := &fakeEC2API{instances: ec2Op[ec2.DescribeInstancesInput]{pages: [][]string{{"i-1", "i-2"}, {"i-3"}}}}

	resources, err := describeEC2Instances(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "ec2-instances"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := instanceIDs(t, resources), []string{"i-1", "i-2", "i-3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
	if len(client.instances.calls) != 2 {
		t.Errorf("expected 2 paginated calls, got %d", len(client.instances.calls))
	}
}

func TestDescribeEC2Instances_NoInstances(t *testing.T) {
	client := &fakeEC2API{}

	resources, err := describeEC2Instances(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "ec2-instances"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// An empty list, not nil, so the JSON output is [] rather than null.
	if resources == nil {
		t.Fatal("expected an empty slice, got nil")
	}
	if len(resources) != 0 {
		t.Errorf("expected no resources, got %d", len(resources))
	}
}

func TestDescribeEC2Instances_TargetIDNarrowsTheRequest(t *testing.T) {
	client := &fakeEC2API{instances: ec2Op[ec2.DescribeInstancesInput]{pages: [][]string{{"i-1"}}}}

	_, err := describeEC2Instances(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "ec2-instances", ID: "i-1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := client.instances.calls[0].InstanceIds, []string{"i-1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected InstanceIds %v, got %v", want, got)
	}
}

func TestDescribeEC2Instances_NoIDListsEverything(t *testing.T) {
	client := &fakeEC2API{instances: ec2Op[ec2.DescribeInstancesInput]{pages: [][]string{{"i-1"}}}}

	_, err := describeEC2Instances(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "ec2-instances"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.instances.calls[0].InstanceIds != nil {
		t.Errorf("expected no InstanceIds, got %v", client.instances.calls[0].InstanceIds)
	}
	if client.instances.calls[0].Filters != nil {
		t.Errorf("expected no Filters, got %v", client.instances.calls[0].Filters)
	}
}

// Filters are pushed down to the API rather than applied after the fact, so
// the call itself has to carry them.
func TestDescribeEC2Instances_FiltersArePushedToTheAPI(t *testing.T) {
	client := &fakeEC2API{instances: ec2Op[ec2.DescribeInstancesInput]{pages: [][]string{{"i-1"}}}}

	_, err := describeEC2Instances(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "ec2-instances"},
		Params: map[string]string{"filters": "instance-state-name=running,instance-state-name=stopped,tag:Name=worker"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []ec2types.Filter{
		{Name: aws.String("instance-state-name"), Values: []string{"running", "stopped"}},
		{Name: aws.String("tag:Name"), Values: []string{"worker"}},
	}
	assertEC2Filters(t, want, client.instances.calls[0].Filters)
}

func TestDescribeEC2Instances_InvalidFilter(t *testing.T) {
	client := &fakeEC2API{instances: ec2Op[ec2.DescribeInstancesInput]{pages: [][]string{{"i-1"}}}}

	_, err := describeEC2Instances(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "ec2-instances"},
		Params: map[string]string{"filters": "instance-state-name"},
	})

	if err == nil {
		t.Fatal("expected an error for a malformed filter, got nil")
	}
	// The request must not reach AWS at all when the caller's input is bad.
	if len(client.instances.calls) != 0 {
		t.Errorf("expected no API calls for a malformed filter, got %d", len(client.instances.calls))
	}
}

// A filters param that parses to nothing must be refused, not treated as
// "no filters". Falling through would call DescribeInstances with neither an
// id nor a filter, listing every instance in the account — the widest possible
// read, produced by input the caller meant to narrow one.
func TestDescribeEC2Instances_EmptyFilterExpressionDoesNotListEverything(t *testing.T) {
	for _, raw := range []string{",", ",,", " , "} {
		t.Run(raw, func(t *testing.T) {
			client := &fakeEC2API{instances: ec2Op[ec2.DescribeInstancesInput]{pages: [][]string{{"i-1"}}}}

			_, err := describeEC2Instances(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
				Target: AWSTarget{Region: "us-east-1", Resource: "ec2-instances"},
				Params: map[string]string{"filters": raw},
			})

			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if len(client.instances.calls) != 0 {
				t.Errorf("expected no API calls, got %d — an unfiltered account-wide list", len(client.instances.calls))
			}
		})
	}
}

// An API failure must name the region, since the same request against a
// different region is a different thing to debug.
func TestDescribeEC2Instances_APIError(t *testing.T) {
	client := &fakeEC2API{err: fmt.Errorf("AccessDenied")}

	_, err := describeEC2Instances(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "eu-west-1", Resource: "ec2-instances"},
	})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := err.Error(), "failed to describe EC2 instances in eu-west-1: AccessDenied"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func assertEC2Filters(t *testing.T, want, got []ec2types.Filter) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d filters, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if aws.ToString(got[i].Name) != aws.ToString(want[i].Name) {
			t.Errorf("filter %d: expected name %q, got %q", i, aws.ToString(want[i].Name), aws.ToString(got[i].Name))
		}
		if !reflect.DeepEqual(got[i].Values, want[i].Values) {
			t.Errorf("filter %d: expected values %v, got %v", i, want[i].Values, got[i].Values)
		}
	}
}

// recordedEC2Call is one request the fake saw, reduced to the two things every
// EC2 read does with caller input: narrowing by id and pushing down filters.
type recordedEC2Call struct {
	ids     []string
	filters []ec2types.Filter
}

// The five non-instance EC2 reads are the same code path with different input
// and output field names, so they are covered by one table rather than five
// near-identical copies of the same assertions — which would make the reads
// look more different from each other than they are.
var ec2ResourceCases = []struct {
	name        string
	describe    func(context.Context, awsapi.Clients, AWSActionRequest) ([]map[string]interface{}, error)
	seed        func(client *fakeEC2API, pages [][]string)
	recorded    func(client *fakeEC2API) []recordedEC2Call
	idKey       string
	errorPrefix string
}{
	{
		name:     "ec2-security-groups",
		describe: describeEC2SecurityGroups,
		seed: func(c *fakeEC2API, pages [][]string) {
			c.securityGroups.pages = pages
		},
		recorded: func(c *fakeEC2API) []recordedEC2Call {
			calls := make([]recordedEC2Call, 0, len(c.securityGroups.calls))
			for _, in := range c.securityGroups.calls {
				calls = append(calls, recordedEC2Call{ids: in.GroupIds, filters: in.Filters})
			}
			return calls
		},
		idKey:       "GroupId",
		errorPrefix: "failed to describe EC2 security groups in",
	},
	{
		name:     "ec2-subnets",
		describe: describeEC2Subnets,
		seed: func(c *fakeEC2API, pages [][]string) {
			c.subnets.pages = pages
		},
		recorded: func(c *fakeEC2API) []recordedEC2Call {
			calls := make([]recordedEC2Call, 0, len(c.subnets.calls))
			for _, in := range c.subnets.calls {
				calls = append(calls, recordedEC2Call{ids: in.SubnetIds, filters: in.Filters})
			}
			return calls
		},
		idKey:       "SubnetId",
		errorPrefix: "failed to describe EC2 subnets in",
	},
	{
		name:     "ec2-route-tables",
		describe: describeEC2RouteTables,
		seed: func(c *fakeEC2API, pages [][]string) {
			c.routeTables.pages = pages
		},
		recorded: func(c *fakeEC2API) []recordedEC2Call {
			calls := make([]recordedEC2Call, 0, len(c.routeTables.calls))
			for _, in := range c.routeTables.calls {
				calls = append(calls, recordedEC2Call{ids: in.RouteTableIds, filters: in.Filters})
			}
			return calls
		},
		idKey:       "RouteTableId",
		errorPrefix: "failed to describe EC2 route tables in",
	},
	{
		name:     "ec2-vpcs",
		describe: describeEC2Vpcs,
		seed: func(c *fakeEC2API, pages [][]string) {
			c.vpcs.pages = pages
		},
		recorded: func(c *fakeEC2API) []recordedEC2Call {
			calls := make([]recordedEC2Call, 0, len(c.vpcs.calls))
			for _, in := range c.vpcs.calls {
				calls = append(calls, recordedEC2Call{ids: in.VpcIds, filters: in.Filters})
			}
			return calls
		},
		idKey:       "VpcId",
		errorPrefix: "failed to describe EC2 VPCs in",
	},
	{
		name:     "ec2-dhcp-options",
		describe: describeEC2DhcpOptions,
		seed: func(c *fakeEC2API, pages [][]string) {
			c.dhcpOptions.pages = pages
		},
		recorded: func(c *fakeEC2API) []recordedEC2Call {
			calls := make([]recordedEC2Call, 0, len(c.dhcpOptions.calls))
			for _, in := range c.dhcpOptions.calls {
				calls = append(calls, recordedEC2Call{ids: in.DhcpOptionsIds, filters: in.Filters})
			}
			return calls
		},
		idKey:       "DhcpOptionsId",
		errorPrefix: "failed to describe EC2 DHCP options in",
	},
}

func TestDescribeEC2Resources_FlattensPages(t *testing.T) {
	for _, tc := range ec2ResourceCases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeEC2API{}
			tc.seed(client, [][]string{{"a", "b"}, {"c"}})

			resources, err := tc.describe(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
				Target: AWSTarget{Region: "us-east-1", Resource: tc.name},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got, want := resourceIDs(t, tc.idKey, resources), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
				t.Errorf("expected %v, got %v", want, got)
			}
			if got := len(tc.recorded(client)); got != 2 {
				t.Errorf("expected 2 paginated calls, got %d", got)
			}
		})
	}
}

func TestDescribeEC2Resources_Empty(t *testing.T) {
	for _, tc := range ec2ResourceCases {
		t.Run(tc.name, func(t *testing.T) {
			resources, err := tc.describe(context.Background(), awsapi.Clients{EC2: &fakeEC2API{}}, AWSActionRequest{
				Target: AWSTarget{Region: "us-east-1", Resource: tc.name},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// An empty list, not nil, so the JSON output is [] rather than null.
			if resources == nil {
				t.Fatal("expected an empty slice, got nil")
			}
			if len(resources) != 0 {
				t.Errorf("expected no resources, got %d", len(resources))
			}
		})
	}
}

func TestDescribeEC2Resources_TargetIDNarrowsTheRequest(t *testing.T) {
	for _, tc := range ec2ResourceCases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeEC2API{}
			tc.seed(client, [][]string{{"a"}})

			_, err := tc.describe(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
				Target: AWSTarget{Region: "us-east-1", Resource: tc.name, ID: "a"},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got, want := tc.recorded(client)[0].ids, []string{"a"}; !reflect.DeepEqual(got, want) {
				t.Errorf("expected ids %v, got %v", want, got)
			}
		})
	}
}

func TestDescribeEC2Resources_NoIDListsEverything(t *testing.T) {
	for _, tc := range ec2ResourceCases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeEC2API{}
			tc.seed(client, [][]string{{"a"}})

			_, err := tc.describe(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
				Target: AWSTarget{Region: "us-east-1", Resource: tc.name},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			call := tc.recorded(client)[0]
			if call.ids != nil {
				t.Errorf("expected no ids, got %v", call.ids)
			}
			if call.filters != nil {
				t.Errorf("expected no filters, got %v", call.filters)
			}
		})
	}
}

func TestDescribeEC2Resources_FiltersArePushedToTheAPI(t *testing.T) {
	for _, tc := range ec2ResourceCases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeEC2API{}
			tc.seed(client, [][]string{{"a"}})

			_, err := tc.describe(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
				Target: AWSTarget{Region: "us-east-1", Resource: tc.name},
				Params: map[string]string{"filters": "vpc-id=vpc-1,tag:Name=worker"},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			want := []ec2types.Filter{
				{Name: aws.String("tag:Name"), Values: []string{"worker"}},
				{Name: aws.String("vpc-id"), Values: []string{"vpc-1"}},
			}
			assertEC2Filters(t, want, tc.recorded(client)[0].filters)
		})
	}
}

func TestDescribeEC2Resources_InvalidFilter(t *testing.T) {
	for _, tc := range ec2ResourceCases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeEC2API{}
			tc.seed(client, [][]string{{"a"}})

			_, err := tc.describe(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
				Target: AWSTarget{Region: "us-east-1", Resource: tc.name},
				Params: map[string]string{"filters": "vpc-id"},
			})

			if err == nil {
				t.Fatal("expected an error for a malformed filter, got nil")
			}
			if got := len(tc.recorded(client)); got != 0 {
				t.Errorf("expected no API calls for a malformed filter, got %d", got)
			}
		})
	}
}

func TestDescribeEC2Resources_APIError(t *testing.T) {
	for _, tc := range ec2ResourceCases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeEC2API{err: fmt.Errorf("AccessDenied")}
			tc.seed(client, [][]string{{"a"}})

			_, err := tc.describe(context.Background(), awsapi.Clients{EC2: client}, AWSActionRequest{
				Target: AWSTarget{Region: "eu-west-1", Resource: tc.name},
			})

			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if got, want := err.Error(), tc.errorPrefix+" eu-west-1: AccessDenied"; got != want {
				t.Errorf("expected %q, got %q", want, got)
			}
		})
	}
}

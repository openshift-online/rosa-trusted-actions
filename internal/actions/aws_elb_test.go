package actions

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

// fakeELBv2API serves the three ELBv2 reads. The load balancer and target
// group reads paginate by an opaque marker, so each is driven from pages the
// way the EC2 fake is; target health returns everything at once.
type fakeELBv2API struct {
	err error

	loadBalancerPages [][]string
	loadBalancerCalls []*elbv2.DescribeLoadBalancersInput

	targetGroupPages [][]string
	targetGroupCalls []*elbv2.DescribeTargetGroupsInput

	targets           []string
	targetHealthCalls []*elbv2.DescribeTargetHealthInput
}

// elbPage returns the names on the page at index, and the marker that asks for
// the next one (nil on the last page).
func elbPage(pages [][]string, index int) ([]string, *string) {
	if index >= len(pages) {
		return nil, nil
	}
	var marker *string
	if index+1 < len(pages) {
		marker = aws.String(fmt.Sprintf("page-%d", index+1))
	}
	return pages[index], marker
}

func (f *fakeELBv2API) DescribeLoadBalancers(_ context.Context, in *elbv2.DescribeLoadBalancersInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error) {
	f.loadBalancerCalls = append(f.loadBalancerCalls, in)
	if f.err != nil {
		return nil, f.err
	}

	names, marker := elbPage(f.loadBalancerPages, len(f.loadBalancerCalls)-1)
	balancers := make([]elbv2types.LoadBalancer, 0, len(names))
	for _, name := range names {
		balancers = append(balancers, elbv2types.LoadBalancer{LoadBalancerName: aws.String(name)})
	}
	return &elbv2.DescribeLoadBalancersOutput{LoadBalancers: balancers, NextMarker: marker}, nil
}

func (f *fakeELBv2API) DescribeTargetGroups(_ context.Context, in *elbv2.DescribeTargetGroupsInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeTargetGroupsOutput, error) {
	f.targetGroupCalls = append(f.targetGroupCalls, in)
	if f.err != nil {
		return nil, f.err
	}

	names, marker := elbPage(f.targetGroupPages, len(f.targetGroupCalls)-1)
	groups := make([]elbv2types.TargetGroup, 0, len(names))
	for _, name := range names {
		groups = append(groups, elbv2types.TargetGroup{TargetGroupName: aws.String(name)})
	}
	return &elbv2.DescribeTargetGroupsOutput{TargetGroups: groups, NextMarker: marker}, nil
}

func (f *fakeELBv2API) DescribeTargetHealth(_ context.Context, in *elbv2.DescribeTargetHealthInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeTargetHealthOutput, error) {
	f.targetHealthCalls = append(f.targetHealthCalls, in)
	if f.err != nil {
		return nil, f.err
	}

	descriptions := make([]elbv2types.TargetHealthDescription, 0, len(f.targets))
	for _, id := range f.targets {
		descriptions = append(descriptions, elbv2types.TargetHealthDescription{
			Target: &elbv2types.TargetDescription{Id: aws.String(id)},
		})
	}
	return &elbv2.DescribeTargetHealthOutput{TargetHealthDescriptions: descriptions}, nil
}

// fakeELBAPI serves the single classic load balancer read.
type fakeELBAPI struct {
	err   error
	pages [][]string
	calls []*elb.DescribeLoadBalancersInput
}

func (f *fakeELBAPI) DescribeLoadBalancers(_ context.Context, in *elb.DescribeLoadBalancersInput, _ ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error) {
	f.calls = append(f.calls, in)
	if f.err != nil {
		return nil, f.err
	}

	names, marker := elbPage(f.pages, len(f.calls)-1)
	balancers := make([]elbtypes.LoadBalancerDescription, 0, len(names))
	for _, name := range names {
		balancers = append(balancers, elbtypes.LoadBalancerDescription{LoadBalancerName: aws.String(name)})
	}
	return &elb.DescribeLoadBalancersOutput{LoadBalancerDescriptions: balancers, NextMarker: marker}, nil
}

// arnOrName is what lets a caller pass whichever identifier they have; if it
// guessed wrong the request would go out with the id in the wrong field and
// AWS would answer about the wrong object.
func TestArnOrName(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		wantArns  []string
		wantNames []string
	}{
		{
			name:     "an ARN goes in the ARN field",
			id:       "arn:aws:elasticloadbalancing:us-east-1:1234:loadbalancer/app/a/b",
			wantArns: []string{"arn:aws:elasticloadbalancing:us-east-1:1234:loadbalancer/app/a/b"},
		},
		{
			name:      "anything else is treated as a name",
			id:        "my-load-balancer",
			wantNames: []string{"my-load-balancer"},
		},
		{
			name:      "a name that merely contains arn is still a name",
			id:        "my-arn-balancer",
			wantNames: []string{"my-arn-balancer"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			arns, names := arnOrName(tc.id)
			if !reflect.DeepEqual(arns, tc.wantArns) {
				t.Errorf("arns: expected %v, got %v", tc.wantArns, arns)
			}
			if !reflect.DeepEqual(names, tc.wantNames) {
				t.Errorf("names: expected %v, got %v", tc.wantNames, names)
			}
		})
	}
}

func TestDescribeLoadBalancers_FlattensPages(t *testing.T) {
	client := &fakeELBv2API{loadBalancerPages: [][]string{{"a", "b"}, {"c"}}}

	resources, err := describeLoadBalancers(context.Background(), awsapi.Clients{ELBv2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "load-balancers"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := resourceIDs(t, "LoadBalancerName", resources), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
	if len(client.loadBalancerCalls) != 2 {
		t.Errorf("expected 2 paginated calls, got %d", len(client.loadBalancerCalls))
	}
}

func TestDescribeLoadBalancers_Empty(t *testing.T) {
	resources, err := describeLoadBalancers(context.Background(), awsapi.Clients{ELBv2: &fakeELBv2API{}}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "load-balancers"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resources == nil {
		t.Fatal("expected an empty slice, got nil")
	}
	if len(resources) != 0 {
		t.Errorf("expected no resources, got %d", len(resources))
	}
}

func TestDescribeLoadBalancers_IDIsRoutedByForm(t *testing.T) {
	arn := "arn:aws:elasticloadbalancing:us-east-1:1234:loadbalancer/app/a/b"

	client := &fakeELBv2API{loadBalancerPages: [][]string{{"a"}}}
	if _, err := describeLoadBalancers(context.Background(), awsapi.Clients{ELBv2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "load-balancers", ID: arn},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := client.loadBalancerCalls[0].LoadBalancerArns, []string{arn}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected LoadBalancerArns %v, got %v", want, got)
	}
	if client.loadBalancerCalls[0].Names != nil {
		t.Errorf("expected no Names, got %v", client.loadBalancerCalls[0].Names)
	}

	client = &fakeELBv2API{loadBalancerPages: [][]string{{"a"}}}
	if _, err := describeLoadBalancers(context.Background(), awsapi.Clients{ELBv2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "load-balancers", ID: "a"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := client.loadBalancerCalls[0].Names, []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected Names %v, got %v", want, got)
	}
	if client.loadBalancerCalls[0].LoadBalancerArns != nil {
		t.Errorf("expected no LoadBalancerArns, got %v", client.loadBalancerCalls[0].LoadBalancerArns)
	}
}

func TestDescribeLoadBalancers_NoIDListsEverything(t *testing.T) {
	client := &fakeELBv2API{loadBalancerPages: [][]string{{"a"}}}

	if _, err := describeLoadBalancers(context.Background(), awsapi.Clients{ELBv2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "load-balancers"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.loadBalancerCalls[0].LoadBalancerArns != nil || client.loadBalancerCalls[0].Names != nil {
		t.Errorf("expected an unnarrowed request, got arns=%v names=%v",
			client.loadBalancerCalls[0].LoadBalancerArns, client.loadBalancerCalls[0].Names)
	}
}

func TestDescribeLoadBalancers_APIError(t *testing.T) {
	_, err := describeLoadBalancers(context.Background(),
		awsapi.Clients{ELBv2: &fakeELBv2API{err: fmt.Errorf("AccessDenied")}}, AWSActionRequest{
			Target: AWSTarget{Region: "eu-west-1", Resource: "load-balancers"},
		})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := err.Error(), "failed to describe load balancers in eu-west-1: AccessDenied"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// Classic load balancers live behind a different API, and the error has to say
// so — otherwise "no load balancers found" reads as if v2 had been checked too.
func TestDescribeClassicLoadBalancers_FlattensPages(t *testing.T) {
	client := &fakeELBAPI{pages: [][]string{{"a", "b"}, {"c"}}}

	resources, err := describeClassicLoadBalancers(context.Background(), awsapi.Clients{ELB: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "load-balancers-classic"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := resourceIDs(t, "LoadBalancerName", resources), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
	if len(client.calls) != 2 {
		t.Errorf("expected 2 paginated calls, got %d", len(client.calls))
	}
}

// The classic API has no ARN field, so the id always goes in as a name.
func TestDescribeClassicLoadBalancers_TargetIDNarrowsTheRequest(t *testing.T) {
	client := &fakeELBAPI{pages: [][]string{{"a"}}}

	if _, err := describeClassicLoadBalancers(context.Background(), awsapi.Clients{ELB: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "load-balancers-classic", ID: "a"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := client.calls[0].LoadBalancerNames, []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected LoadBalancerNames %v, got %v", want, got)
	}
}

func TestDescribeClassicLoadBalancers_NoIDListsEverything(t *testing.T) {
	client := &fakeELBAPI{pages: [][]string{{"a"}}}

	if _, err := describeClassicLoadBalancers(context.Background(), awsapi.Clients{ELB: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "load-balancers-classic"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.calls[0].LoadBalancerNames != nil {
		t.Errorf("expected no LoadBalancerNames, got %v", client.calls[0].LoadBalancerNames)
	}
}

func TestDescribeClassicLoadBalancers_APIError(t *testing.T) {
	_, err := describeClassicLoadBalancers(context.Background(),
		awsapi.Clients{ELB: &fakeELBAPI{err: fmt.Errorf("AccessDenied")}}, AWSActionRequest{
			Target: AWSTarget{Region: "eu-west-1", Resource: "load-balancers-classic"},
		})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := err.Error(), "failed to describe classic load balancers in eu-west-1: AccessDenied"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestDescribeTargetGroups_FlattensPages(t *testing.T) {
	client := &fakeELBv2API{targetGroupPages: [][]string{{"a", "b"}, {"c"}}}

	resources, err := describeTargetGroups(context.Background(), awsapi.Clients{ELBv2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "target-groups"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := resourceIDs(t, "TargetGroupName", resources), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
	if len(client.targetGroupCalls) != 2 {
		t.Errorf("expected 2 paginated calls, got %d", len(client.targetGroupCalls))
	}
}

func TestDescribeTargetGroups_IDIsRoutedByForm(t *testing.T) {
	arn := "arn:aws:elasticloadbalancing:us-east-1:1234:targetgroup/a/b"

	client := &fakeELBv2API{targetGroupPages: [][]string{{"a"}}}
	if _, err := describeTargetGroups(context.Background(), awsapi.Clients{ELBv2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "target-groups", ID: arn},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := client.targetGroupCalls[0].TargetGroupArns, []string{arn}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected TargetGroupArns %v, got %v", want, got)
	}

	client = &fakeELBv2API{targetGroupPages: [][]string{{"a"}}}
	if _, err := describeTargetGroups(context.Background(), awsapi.Clients{ELBv2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "target-groups", ID: "a"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := client.targetGroupCalls[0].Names, []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected Names %v, got %v", want, got)
	}
}

func TestDescribeTargetGroups_APIError(t *testing.T) {
	_, err := describeTargetGroups(context.Background(),
		awsapi.Clients{ELBv2: &fakeELBv2API{err: fmt.Errorf("AccessDenied")}}, AWSActionRequest{
			Target: AWSTarget{Region: "eu-west-1", Resource: "target-groups"},
		})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := err.Error(), "failed to describe target groups in eu-west-1: AccessDenied"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// The id is the target group to report on, so it has to reach the API as the
// TargetGroupArn — not as a filter applied afterwards.
func TestDescribeTargetHealth_ReadsTheNamedGroup(t *testing.T) {
	arn := "arn:aws:elasticloadbalancing:us-east-1:1234:targetgroup/a/b"
	client := &fakeELBv2API{targets: []string{"i-1", "i-2"}}

	resources, err := describeTargetHealth(context.Background(), awsapi.Clients{ELBv2: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "target-health", ID: arn},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(client.targetHealthCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(client.targetHealthCalls))
	}
	if got := aws.ToString(client.targetHealthCalls[0].TargetGroupArn); got != arn {
		t.Errorf("expected TargetGroupArn %q, got %q", arn, got)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 target health descriptions, got %d", len(resources))
	}
}

func TestDescribeTargetHealth_Empty(t *testing.T) {
	resources, err := describeTargetHealth(context.Background(), awsapi.Clients{ELBv2: &fakeELBv2API{}}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "target-health", ID: "tg"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resources == nil {
		t.Fatal("expected an empty slice, got nil")
	}
	if len(resources) != 0 {
		t.Errorf("expected no resources, got %d", len(resources))
	}
}

// The failure has to name the target group as well as the region: the caller
// asked about one specific group, and "in eu-west-1" alone would not say which.
func TestDescribeTargetHealth_APIError(t *testing.T) {
	_, err := describeTargetHealth(context.Background(),
		awsapi.Clients{ELBv2: &fakeELBv2API{err: fmt.Errorf("TargetGroupNotFound")}}, AWSActionRequest{
			Target: AWSTarget{Region: "eu-west-1", Resource: "target-health", ID: "tg-1"},
		})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := err.Error(), "failed to describe target health for tg-1 in eu-west-1: TargetGroupNotFound"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

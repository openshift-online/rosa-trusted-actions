package actions

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

func TestAWSGetAction_Name(t *testing.T) {
	if got := NewAWSGetAction().Name(); got != "aws-get" {
		t.Errorf("expected name %q, got %q", "aws-get", got)
	}
}

// Every descriptor must declare the client its describe function actually
// reaches for, otherwise the execution gets a Clients with a nil field.
func TestAWSGetAction_RequiredServices(t *testing.T) {
	action := NewAWSGetAction()

	services := action.RequiredServices(AWSTarget{Resource: "ec2-instances"})
	if !reflect.DeepEqual(services, []awsapi.Service{awsapi.ServiceEC2}) {
		t.Errorf("expected [ec2], got %v", services)
	}

	if services := action.RequiredServices(AWSTarget{Resource: "nope"}); services != nil {
		t.Errorf("expected no services for an unknown resource, got %v", services)
	}

	// One service per resource, always: asking for more would hand the
	// execution access it has no reason to use.
	for name, descriptor := range awsResources {
		got := action.RequiredServices(AWSTarget{Resource: name})
		if !reflect.DeepEqual(got, []awsapi.Service{descriptor.service}) {
			t.Errorf("resource %q: expected [%s], got %v", name, descriptor.service, got)
		}
	}
}

func TestAWSGetAction_UnknownResource(t *testing.T) {
	action := NewAWSGetAction()

	_, err := action.Execute(context.Background(), awsapi.Clients{}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "s3-buckets"},
	})

	if err == nil {
		t.Fatal("expected an error for an unknown resource, got nil")
	}
	// The message must list what is available; a bare rejection leaves the
	// caller guessing at the spelling.
	if !strings.Contains(err.Error(), "ec2-instances") {
		t.Errorf("expected the error to list the known resources, got %q", err.Error())
	}
}

func TestAWSGetAction_ListMessage(t *testing.T) {
	clients := awsapi.Clients{EC2: &fakeEC2API{instances: ec2Op[ec2.DescribeInstancesInput]{pages: [][]string{{"i-1", "i-2"}}}}}

	result, err := NewAWSGetAction().Execute(context.Background(), clients, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "ec2-instances"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := result.Message, "listed 2 ec2-instances in us-east-1"; got != want {
		t.Errorf("expected message %q, got %q", want, got)
	}
}

func TestAWSGetAction_GetMessage(t *testing.T) {
	clients := awsapi.Clients{EC2: &fakeEC2API{instances: ec2Op[ec2.DescribeInstancesInput]{pages: [][]string{{"i-1"}}}}}

	result, err := NewAWSGetAction().Execute(context.Background(), clients, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "ec2-instances", ID: "i-1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := result.Message, "got ec2-instances/i-1 in us-east-1"; got != want {
		t.Errorf("expected message %q, got %q", want, got)
	}
}

// When the id names a parent rather than the object returned, "got
// target-health/<arn>" would describe the wrong thing — what came back is a
// list of that group's targets.
func TestAWSGetAction_RequiredIDMessageIsAListing(t *testing.T) {
	clients := awsapi.Clients{ELBv2: &fakeELBv2API{targets: []string{"i-1", "i-2"}}}

	result, err := NewAWSGetAction().Execute(context.Background(), clients, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "target-health", ID: "tg-1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := result.Message, "listed 2 target-health for tg-1 in us-east-1"; got != want {
		t.Errorf("expected message %q, got %q", want, got)
	}
}

// A resource whose AWS call needs a parent id has nothing to return without
// one, so the request is refused with a message saying what the id should be.
func TestAWSGetAction_MissingRequiredID(t *testing.T) {
	for _, resource := range []string{"target-health", "route53-record-sets"} {
		t.Run(resource, func(t *testing.T) {
			_, err := NewAWSGetAction().Execute(context.Background(), awsapi.Clients{}, AWSActionRequest{
				Target: AWSTarget{Region: "us-east-1", Resource: resource},
			})

			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), "requires an id") {
				t.Errorf("expected the error to say an id is required, got %q", err.Error())
			}
			if !strings.Contains(err.Error(), awsResources[resource].idDescription) {
				t.Errorf("expected the error to say what the id is, got %q", err.Error())
			}
		})
	}
}

// Rejected rather than ignored: a caller who passes an id believes the result
// is narrowed to it, and silently listing everything instead is the kind of
// answer they would act on without noticing.
func TestAWSGetAction_UnsupportedID(t *testing.T) {
	for _, resource := range []string{"cloudtrail-events", "route53-hosted-zones"} {
		t.Run(resource, func(t *testing.T) {
			_, err := NewAWSGetAction().Execute(context.Background(), awsapi.Clients{}, AWSActionRequest{
				Target: AWSTarget{Region: "us-east-1", Resource: resource, ID: "whatever"},
			})

			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), "cannot be read by id") {
				t.Errorf("expected the error to reject the id, got %q", err.Error())
			}
		})
	}
}

// Same reasoning for params: an ignored "filters" returns a wider result set
// than the caller asked for, and nothing in the response would say so.
func TestAWSGetAction_UnknownParam(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		params   map[string]string
		wants    []string
	}{
		{
			name:     "a param this resource does not take",
			resource: "ec2-instances",
			params:   map[string]string{"since": "2h"},
			wants:    []string{`"since"`, "it accepts: filters"},
		},
		{
			name:     "a param no resource takes",
			resource: "ec2-instances",
			params:   map[string]string{"namespace": "default"},
			wants:    []string{`"namespace"`},
		},
		{
			name:     "filters on a resource that cannot filter",
			resource: "load-balancers",
			params:   map[string]string{"filters": "tag:Name=a"},
			wants:    []string{`"filters"`, "it accepts no params"},
		},
		{
			name:     "since on a resource that has no window",
			resource: "route53-hosted-zones",
			params:   map[string]string{"since": "2h"},
			wants:    []string{`"since"`, "it accepts no params"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewAWSGetAction().Execute(context.Background(), awsapi.Clients{}, AWSActionRequest{
				Target: AWSTarget{Region: "us-east-1", Resource: tc.resource},
				Params: tc.params,
			})

			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			for _, want := range tc.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected the error to mention %q, got %q", want, err.Error())
				}
			}
		})
	}
}

// The params that build the target arrive in the same map as the
// resource-specific ones, so they must not trip the unknown-param check.
func TestAWSGetAction_TargetParamsAreNotRejected(t *testing.T) {
	clients := awsapi.Clients{EC2: &fakeEC2API{instances: ec2Op[ec2.DescribeInstancesInput]{pages: [][]string{{"i-1"}}}}}

	_, err := NewAWSGetAction().Execute(context.Background(), clients, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "ec2-instances", ID: "i-1"},
		Params: map[string]string{"resource": "ec2-instances", "id": "i-1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The region is resolved from the cluster, not sent by the caller, so it is
// not a target param and must be refused like any other unrecognised one.
func TestAWSGetAction_RegionParamIsRejected(t *testing.T) {
	clients := awsapi.Clients{EC2: &fakeEC2API{instances: ec2Op[ec2.DescribeInstancesInput]{pages: [][]string{{"i-1"}}}}}

	_, err := NewAWSGetAction().Execute(context.Background(), clients, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "ec2-instances"},
		Params: map[string]string{"resource": "ec2-instances", "region": "eu-west-1"},
	})
	if err == nil {
		t.Fatal("expected a region param to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "region") {
		t.Errorf("expected the error to name the offending param, got %q", err)
	}
}

// A param a resource does not accept is only a problem when it was actually
// set. Clients that send every field of a form — action-cli does — would
// otherwise be refused for params they left blank.
func TestAWSGetAction_BlankParamIsNotRejected(t *testing.T) {
	clients := awsapi.Clients{Route53: &fakeRoute53API{zonePages: [][]string{{"Z1"}}}}

	result, err := NewAWSGetAction().Execute(context.Background(), clients, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "route53-hosted-zones"},
		Params: map[string]string{"filters": "", "since": "   ", "id": ""},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Errorf("expected the read to run, got %d resources", len(result.Resources))
	}

	// Still rejected once it carries a value, which is the case that matters.
	if _, err := NewAWSGetAction().Execute(context.Background(), clients, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "route53-hosted-zones"},
		Params: map[string]string{"filters": "tag:Name=a"},
	}); err == nil {
		t.Error("expected a set param the resource does not accept to be rejected")
	}
}

// Several bad params must always produce the same error, so a failure reads
// the same way twice and an audit entry is reproducible.
func TestAWSGetAction_UnknownParamErrorIsDeterministic(t *testing.T) {
	req := AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "ec2-instances"},
		Params: map[string]string{"zebra": "1", "alpha": "2", "mike": "3"},
	}

	first, err := NewAWSGetAction().Execute(context.Background(), awsapi.Clients{}, req)
	if err == nil {
		t.Fatalf("expected an error, got %+v", first)
	}
	for i := 0; i < 20; i++ {
		_, again := NewAWSGetAction().Execute(context.Background(), awsapi.Clients{}, req)
		if again == nil || again.Error() != err.Error() {
			t.Fatalf("expected a stable error %q, got %v", err.Error(), again)
		}
	}
	// Sorted, so the first offending name is reported.
	if !strings.Contains(err.Error(), `"alpha"`) {
		t.Errorf("expected the alphabetically first unknown param, got %q", err.Error())
	}
}

func TestParseAWSFilters(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []awsFilter
		wantErr bool
	}{
		{
			name: "empty",
			raw:  "",
			want: nil,
		},
		{
			name: "blank",
			raw:  "   ",
			want: nil,
		},
		{
			name: "single",
			raw:  "instance-state-name=running",
			want: []awsFilter{{Name: "instance-state-name", Values: []string{"running"}}},
		},
		{
			// Ordered by name so identical params always produce the same call.
			name: "sorted by name",
			raw:  "tag:Name=worker,instance-type=m5.xlarge",
			want: []awsFilter{
				{Name: "instance-type", Values: []string{"m5.xlarge"}},
				{Name: "tag:Name", Values: []string{"worker"}},
			},
		},
		{
			name: "repeated name accumulates values",
			raw:  "instance-state-name=running,instance-state-name=stopped",
			want: []awsFilter{{Name: "instance-state-name", Values: []string{"running", "stopped"}}},
		},
		{
			name: "surrounding whitespace is trimmed",
			raw:  " instance-state-name = running ",
			want: []awsFilter{{Name: "instance-state-name", Values: []string{"running"}}},
		},
		{
			// An empty value is a legitimate AWS filter, e.g. matching an
			// unset tag, so it must not be rejected.
			name: "empty value is kept",
			raw:  "tag:Name=",
			want: []awsFilter{{Name: "tag:Name", Values: []string{""}}},
		},
		{
			name: "trailing separator is ignored",
			raw:  "instance-state-name=running,",
			want: []awsFilter{{Name: "instance-state-name", Values: []string{"running"}}},
		},
		{
			name:    "missing equals",
			raw:     "instance-state-name",
			wantErr: true,
		},
		{
			name:    "missing name",
			raw:     "=running",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAWSFilters(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q, got %v", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("expected %+v, got %+v", tt.want, got)
			}
		})
	}
}

func TestAWSResourceNames(t *testing.T) {
	names := AWSResourceNames()
	if len(names) != len(awsResources) {
		t.Fatalf("expected %d names, got %d", len(awsResources), len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("expected sorted names, got %v", names)
			break
		}
	}
}

// Every descriptor must be wired up; a nil describe would panic at execution
// time rather than being caught here.
func TestAWSResources_AllDescriptorsComplete(t *testing.T) {
	for name, descriptor := range awsResources {
		if descriptor.service == "" {
			t.Errorf("resource %q declares no AWS service", name)
		}
		if descriptor.describe == nil {
			t.Errorf("resource %q has no describe function", name)
		}
		// Without this the required-id error says "requires an id:" and stops,
		// leaving the caller no idea what to supply.
		if descriptor.id == awsIDRequired && descriptor.idDescription == "" {
			t.Errorf("resource %q requires an id but does not say what it is", name)
		}
		if descriptor.id != awsIDRequired && descriptor.idDescription != "" {
			t.Errorf("resource %q describes an id it does not require", name)
		}
	}
}

// Every resource's declared service must be one the provider can actually
// build, and it must arrive in the Clients field the read looks at. A
// mismatch stays invisible until an execution reaches AWS and dereferences a
// nil client.
func TestAWSResources_DeclaredServicesAreProvisionable(t *testing.T) {
	// Isolated from whatever AWS configuration the host has, so the result
	// depends only on the provider's own switch.
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_PROFILE", "")

	built := map[awsapi.Service]func(awsapi.Clients) bool{
		awsapi.ServiceEC2:        func(c awsapi.Clients) bool { return c.EC2 != nil },
		awsapi.ServiceELB:        func(c awsapi.Clients) bool { return c.ELB != nil },
		awsapi.ServiceELBv2:      func(c awsapi.Clients) bool { return c.ELBv2 != nil },
		awsapi.ServiceCloudTrail: func(c awsapi.Clients) bool { return c.CloudTrail != nil },
		awsapi.ServiceRoute53:    func(c awsapi.Clients) bool { return c.Route53 != nil },
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	provider := awsapi.NewStaticProvider(logger, awsapi.StaticConfig{
		Region:          "us-east-1",
		AccessKeyID:     "test",
		SecretAccessKey: "test",
	})

	for name, descriptor := range awsResources {
		isBuilt, known := built[descriptor.service]
		if !known {
			t.Errorf("resource %q declares service %q, which this test does not cover", name, descriptor.service)
			continue
		}

		clients, err := provider.GetClients(context.Background(), "cluster-1", "us-east-1", []awsapi.Service{descriptor.service})
		if err != nil {
			t.Errorf("resource %q: the provider cannot build service %q: %v", name, descriptor.service, err)
			continue
		}
		if !isBuilt(clients) {
			t.Errorf("resource %q: the provider built no client for service %q", name, descriptor.service)
		}
	}
}

// awsResourcesFrom and pruneUnset live in actions.go alongside the Kubernetes
// conversion, but they are only used by the AWS actions, so their tests sit
// here with the rest of the AWS surface.

func TestAWSResourcesFrom_UsesAPIFieldNames(t *testing.T) {
	type instance struct {
		InstanceId   *string
		InstanceType string
	}
	id := "i-1"

	resources, err := awsResourcesFrom([]instance{{InstanceId: &id, InstanceType: "m5.large"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	// The SDK types carry no JSON tags, so the keys are the API's own field
	// names — the shape the AWS CLI prints.
	if got := resources[0]["InstanceId"]; got != "i-1" {
		t.Errorf("expected InstanceId %q, got %v", "i-1", got)
	}
	if got := resources[0]["InstanceType"]; got != "m5.large" {
		t.Errorf("expected InstanceType %q, got %v", "m5.large", got)
	}
}

func TestAWSResourcesFrom_Empty(t *testing.T) {
	resources, err := awsResourcesFrom([]struct{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty rather than nil, so the output serializes as [] not null.
	if resources == nil {
		t.Fatal("expected an empty slice, got nil")
	}
	if len(resources) != 0 {
		t.Errorf("expected no resources, got %d", len(resources))
	}
}

// A nil pointer in the SDK means the API did not set the field, which is not
// the same as the field being null — dropping it keeps the output honest and
// far shorter, since most fields of an EC2 instance are unset.
func TestAWSResourcesFrom_DropsUnsetFields(t *testing.T) {
	type instance struct {
		InstanceId *string
		KeyName    *string
	}
	id := "i-1"

	resources, err := awsResourcesFrom([]instance{{InstanceId: &id}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, present := resources[0]["KeyName"]; present {
		t.Errorf("expected the unset KeyName to be dropped, got %#v", resources[0])
	}
	if _, present := resources[0]["InstanceId"]; !present {
		t.Error("expected the set InstanceId to be kept")
	}
}

func TestPruneUnset_Nested(t *testing.T) {
	object := map[string]interface{}{
		"Top":     "value",
		"Missing": nil,
		"State": map[string]interface{}{
			"Name": "running",
			"Code": nil,
		},
		"Tags": []interface{}{
			map[string]interface{}{"Key": "Name", "Value": nil},
			[]interface{}{
				map[string]interface{}{"Nested": nil, "Kept": 1},
			},
		},
	}

	pruned := pruneUnset(object)

	if _, present := pruned["Missing"]; present {
		t.Error("expected the top-level nil to be dropped")
	}
	state := pruned["State"].(map[string]interface{})
	if _, present := state["Code"]; present {
		t.Error("expected the nested nil to be dropped")
	}
	if state["Name"] != "running" {
		t.Errorf("expected the nested value to be kept, got %v", state["Name"])
	}

	tags := pruned["Tags"].([]interface{})
	tag := tags[0].(map[string]interface{})
	if _, present := tag["Value"]; present {
		t.Error("expected the nil inside a list element to be dropped")
	}
	inner := tags[1].([]interface{})[0].(map[string]interface{})
	if _, present := inner["Nested"]; present {
		t.Error("expected the nil inside a nested list to be dropped")
	}
	if inner["Kept"] != 1 {
		t.Errorf("expected the value inside a nested list to be kept, got %v", inner["Kept"])
	}
}

package actions

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

const testInfraID = "my-cluster-abcde"

// ownedInstance is an instance tagged as belonging to testInfraID, i.e. one a
// write action should accept.
func ownedInstance(id string) ec2types.Instance {
	return taggedInstance(id, clusterOwnershipTag(testInfraID), clusterOwnershipTagValue)
}

func taggedInstance(id, key, value string) ec2types.Instance {
	return ec2types.Instance{
		InstanceId: aws.String(id),
		Tags:       []ec2types.Tag{{Key: aws.String(key), Value: aws.String(value)}},
	}
}

func writeRequest(id string) AWSActionRequest {
	return AWSActionRequest{Target: AWSTarget{
		Region:   "us-east-1",
		Resource: awsWriteResourceInstances,
		ID:       id,
		InfraID:  testInfraID,
	}}
}

func TestAWSRebootAction_Identity(t *testing.T) {
	action := NewAWSRebootAction()

	if action.Name() != "aws-reboot" {
		t.Errorf("Name() = %q, want %q", action.Name(), "aws-reboot")
	}
	if action.Operation() != AWSOperationReboot {
		t.Errorf("Operation() = %q, want %q", action.Operation(), AWSOperationReboot)
	}
	// The resource is the action's to decide, and must be the same name
	// aws-get uses, so "ec2-instances" means one thing across the API.
	if action.Resource() != "ec2-instances" {
		t.Errorf("Resource() = %q, want %q", action.Resource(), "ec2-instances")
	}
	if _, ok := awsResources[action.Resource()]; !ok {
		t.Errorf("Resource() = %q, which aws-get does not know about", action.Resource())
	}

	services := action.RequiredServices(AWSTarget{})
	if len(services) != 1 || services[0] != awsapi.ServiceEC2 {
		t.Errorf("RequiredServices() = %v, want [ec2]", services)
	}
}

func TestAWSTerminateAction_Identity(t *testing.T) {
	action := NewAWSTerminateAction()

	if action.Name() != "aws-terminate" {
		t.Errorf("Name() = %q, want %q", action.Name(), "aws-terminate")
	}
	if action.Operation() != AWSOperationTerminate {
		t.Errorf("Operation() = %q, want %q", action.Operation(), AWSOperationTerminate)
	}
	if action.Resource() != "ec2-instances" {
		t.Errorf("Resource() = %q, want %q", action.Resource(), "ec2-instances")
	}
	if _, ok := awsResources[action.Resource()]; !ok {
		t.Errorf("Resource() = %q, which aws-get does not know about", action.Resource())
	}

	// Checked here as well as on reboot: the scope builds only the clients an
	// action declares, so naming the wrong service would hand this one a client
	// it cannot terminate with, and the failure would surface at run time
	// against a real instance rather than here.
	services := action.RequiredServices(AWSTarget{})
	if len(services) != 1 || services[0] != awsapi.ServiceEC2 {
		t.Errorf("RequiredServices() = %v, want [ec2]", services)
	}
}

// Both must satisfy AWSWriteAction, because that interface is the only thing
// telling the scope to apply the write allowlist. An action that fell back to
// plain AWSAction would still run — as a read, ungated.
func TestAWSWriteActions_ImplementWriteInterface(t *testing.T) {
	for _, action := range []Action{NewAWSRebootAction(), NewAWSTerminateAction()} {
		if _, ok := action.(AWSWriteAction); !ok {
			t.Errorf("%s does not implement AWSWriteAction, so it would be authorized as a read", action.Name())
		}
	}
}

func TestAWSWriteActions_Registered(t *testing.T) {
	for _, name := range []string{"aws-reboot", "aws-terminate"} {
		action := CreateAction(name)
		if action == nil {
			t.Fatalf("CreateAction(%q) = nil, want the registered action", name)
		}
		if action.Name() != name {
			t.Errorf("CreateAction(%q).Name() = %q", name, action.Name())
		}
	}
}

func TestAWSRebootAction_Execute(t *testing.T) {
	client := &fakeEC2API{instanceDetails: []ec2types.Instance{ownedInstance("i-123")}}

	result, err := NewAWSRebootAction().Execute(context.Background(),
		awsapi.Clients{EC2: client}, writeRequest("i-123"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(client.rebooted) != 1 {
		t.Fatalf("RebootInstances called %d times, want 1", len(client.rebooted))
	}
	if ids := client.rebooted[0].InstanceIds; len(ids) != 1 || ids[0] != "i-123" {
		t.Errorf("rebooted %v, want [i-123]", ids)
	}
	if !strings.Contains(result.Message, "i-123") || !strings.Contains(result.Message, "us-east-1") {
		t.Errorf("Message = %q, want it to name the instance and region", result.Message)
	}

	// The instance that was acted on is returned, so the stored output records
	// what the write hit rather than only the id that was asked for.
	if len(result.Resources) != 1 {
		t.Fatalf("Resources = %d, want 1", len(result.Resources))
	}
	if got := result.Resources[0]["InstanceId"]; got != "i-123" {
		t.Errorf("Resources[0][InstanceId] = %v, want i-123", got)
	}
}

func TestAWSTerminateAction_Execute(t *testing.T) {
	client := &fakeEC2API{
		instanceDetails: []ec2types.Instance{ownedInstance("i-123")},
		terminatingState: &ec2types.InstanceStateChange{
			InstanceId:    aws.String("i-123"),
			PreviousState: &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
			CurrentState:  &ec2types.InstanceState{Name: ec2types.InstanceStateNameShuttingDown},
		},
	}

	result, err := NewAWSTerminateAction().Execute(context.Background(),
		awsapi.Clients{EC2: client}, writeRequest("i-123"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(client.terminated) != 1 {
		t.Fatalf("TerminateInstances called %d times, want 1", len(client.terminated))
	}
	if ids := client.terminated[0].InstanceIds; len(ids) != 1 || ids[0] != "i-123" {
		t.Errorf("terminated %v, want [i-123]", ids)
	}
	// The transition distinguishes a termination that took effect from one
	// issued against an instance that was already going away.
	if !strings.Contains(result.Message, "running -> shutting-down") {
		t.Errorf("Message = %q, want it to report the state transition", result.Message)
	}
}

// AWS does not always answer with a state change. The message should lose the
// transition, not the rest of itself.
func TestAWSTerminateAction_ExecuteWithoutStateChange(t *testing.T) {
	client := &fakeEC2API{instanceDetails: []ec2types.Instance{ownedInstance("i-123")}}

	result, err := NewAWSTerminateAction().Execute(context.Background(),
		awsapi.Clients{EC2: client}, writeRequest("i-123"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Message, "i-123") {
		t.Errorf("Message = %q, want it to name the instance", result.Message)
	}
	if strings.Contains(result.Message, "->") {
		t.Errorf("Message = %q, want no transition when AWS reported none", result.Message)
	}
}

// A state change with the states themselves unset. Both fields are pointers in
// the SDK, so this is a shape AWS is free to return; reporting the termination
// without a transition is the right answer, and dereferencing them would turn
// a successful write into a panic after the instance is already gone.
func TestAWSTerminateAction_ExecuteWithPartialStateChange(t *testing.T) {
	client := &fakeEC2API{
		instanceDetails:  []ec2types.Instance{ownedInstance("i-123")},
		terminatingState: &ec2types.InstanceStateChange{InstanceId: aws.String("i-123")},
	}

	result, err := NewAWSTerminateAction().Execute(context.Background(),
		awsapi.Clients{EC2: client}, writeRequest("i-123"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(client.terminated) != 1 {
		t.Fatalf("terminated %d instances, want 1", len(client.terminated))
	}
	if !strings.Contains(result.Message, "i-123") {
		t.Errorf("Message = %q, want it to name the instance", result.Message)
	}
	if strings.Contains(result.Message, "->") {
		t.Errorf("Message = %q, want no transition when AWS reported no states", result.Message)
	}
}

// The ownership check, which is what bounds a write's blast radius to the
// cluster rather than its whole AWS account. Every case here must refuse
// before any mutating call is made — a write that happens and then reports an
// error is not a refusal.
func TestAWSWriteActions_OwnershipCheck(t *testing.T) {
	otherCluster := clusterOwnershipTag("someone-elses-cluster-fghij")

	tests := []struct {
		name      string
		target    AWSTarget
		instances []ec2types.Instance
		wantErr   string
	}{
		{
			name:      "instance belongs to another cluster",
			target:    writeRequest("i-123").Target,
			instances: []ec2types.Instance{taggedInstance("i-123", otherCluster, clusterOwnershipTagValue)},
			wantErr:   "does not belong to cluster",
		},
		{
			name:      "instance carries no cluster tag at all",
			target:    writeRequest("i-123").Target,
			instances: []ec2types.Instance{{InstanceId: aws.String("i-123")}},
			wantErr:   "does not belong to cluster",
		},
		{
			// "shared" marks a resource the cluster uses but does not own, so
			// something else is relying on it. Reboot it and that something
			// else finds out the hard way.
			name:      "instance is shared rather than owned",
			target:    writeRequest("i-123").Target,
			instances: []ec2types.Instance{taggedInstance("i-123", clusterOwnershipTag(testInfraID), "shared")},
			wantErr:   "does not belong to cluster",
		},
		{
			// A different instance coming back is not an answer about the one
			// that was asked for, and it is the asked-for id that gets written.
			name:      "a different instance came back",
			target:    writeRequest("i-123").Target,
			instances: []ec2types.Instance{ownedInstance("i-999")},
			wantErr:   "was not found",
		},
		{
			name:      "no instance came back",
			target:    writeRequest("i-123").Target,
			instances: []ec2types.Instance{},
			wantErr:   "was not found",
		},
		{
			// An ownership check with nothing to compare against has not
			// passed, so it must refuse rather than skip.
			name:      "the cluster's identity is unknown",
			target:    AWSTarget{Region: "us-east-1", Resource: awsWriteResourceInstances, ID: "i-123"},
			instances: []ec2types.Instance{ownedInstance("i-123")},
			wantErr:   "no infrastructure ID is known",
		},
		{
			name:      "no instance was named",
			target:    AWSTarget{Region: "us-east-1", Resource: awsWriteResourceInstances, InfraID: testInfraID},
			instances: []ec2types.Instance{ownedInstance("i-123")},
			wantErr:   "an instance id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, action := range []AWSWriteAction{NewAWSRebootAction(), NewAWSTerminateAction()} {
				client := &fakeEC2API{instanceDetails: tt.instances}

				_, err := action.Execute(context.Background(),
					awsapi.Clients{EC2: client}, AWSActionRequest{Target: tt.target})

				if err == nil {
					t.Fatalf("%s: Execute() error = nil, want a refusal", action.Name())
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("%s: Execute() error = %q, want it to contain %q", action.Name(), err, tt.wantErr)
				}
				if len(client.rebooted) != 0 || len(client.terminated) != 0 {
					t.Errorf("%s: refused the write but still called AWS (%d reboots, %d terminations)",
						action.Name(), len(client.rebooted), len(client.terminated))
				}
			}
		})
	}
}

// An instance can carry many tags; the ownership one must be found among them
// rather than only when it happens to be first.
func TestAWSWriteActions_OwnershipTagAmongOthers(t *testing.T) {
	client := &fakeEC2API{instanceDetails: []ec2types.Instance{{
		InstanceId: aws.String("i-123"),
		Tags: []ec2types.Tag{
			{Key: aws.String("Name"), Value: aws.String("my-cluster-worker-1")},
			{Key: aws.String("kubernetes.io/cluster/some-other-cluster"), Value: aws.String("shared")},
			{Key: aws.String(clusterOwnershipTag(testInfraID)), Value: aws.String(clusterOwnershipTagValue)},
		},
	}}}

	if _, err := NewAWSRebootAction().Execute(context.Background(),
		awsapi.Clients{EC2: client}, writeRequest("i-123")); err != nil {
		t.Fatalf("Execute() error = %v, want the ownership tag to be found among the others", err)
	}
	if len(client.rebooted) != 1 {
		t.Errorf("RebootInstances called %d times, want 1", len(client.rebooted))
	}
}

// A failed lookup is not a passed ownership check either.
func TestAWSWriteActions_DescribeFailureRefusesWrite(t *testing.T) {
	for _, action := range []AWSWriteAction{NewAWSRebootAction(), NewAWSTerminateAction()} {
		client := &fakeEC2API{err: errors.New("boom")}

		_, err := action.Execute(context.Background(),
			awsapi.Clients{EC2: client}, writeRequest("i-123"))

		if err == nil {
			t.Fatalf("%s: Execute() error = nil, want the describe failure to stop the write", action.Name())
		}
		if len(client.rebooted) != 0 || len(client.terminated) != 0 {
			t.Errorf("%s: wrote to AWS despite failing to verify ownership", action.Name())
		}
	}
}

func TestAWSWriteActions_WriteFailureIsReported(t *testing.T) {
	reboot := &fakeEC2API{
		instanceDetails: []ec2types.Instance{ownedInstance("i-123")},
		rebootErr:       errors.New("boom"),
	}
	if _, err := NewAWSRebootAction().Execute(context.Background(),
		awsapi.Clients{EC2: reboot}, writeRequest("i-123")); err == nil {
		t.Error("Execute() error = nil, want the reboot failure reported")
	}

	terminate := &fakeEC2API{
		instanceDetails: []ec2types.Instance{ownedInstance("i-123")},
		terminateErr:    errors.New("boom"),
	}
	if _, err := NewAWSTerminateAction().Execute(context.Background(),
		awsapi.Clients{EC2: terminate}, writeRequest("i-123")); err == nil {
		t.Error("Execute() error = nil, want the termination failure reported")
	}
}

// The tag has to be the one OpenShift actually writes, and the one CAD reads.
// Getting it wrong would not fail loudly — it would refuse every legitimate
// write, or, if it matched something broader, permit the wrong ones.
func TestClusterOwnershipTag(t *testing.T) {
	if got := clusterOwnershipTag("my-cluster-abcde"); got != "kubernetes.io/cluster/my-cluster-abcde" {
		t.Errorf("clusterOwnershipTag() = %q, want %q", got, "kubernetes.io/cluster/my-cluster-abcde")
	}
	if clusterOwnershipTagValue != "owned" {
		t.Errorf("clusterOwnershipTagValue = %q, want %q", clusterOwnershipTagValue, "owned")
	}
}

// The describe is what the ownership check reads, so it has to ask about the
// instance in the target and not, say, list the account.
func TestAWSWriteActions_DescribeIsNarrowedToTheInstance(t *testing.T) {
	client := &fakeEC2API{instanceDetails: []ec2types.Instance{ownedInstance("i-123")}}

	if _, err := NewAWSRebootAction().Execute(context.Background(),
		awsapi.Clients{EC2: client}, writeRequest("i-123")); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	calls := client.instances.calls
	if len(calls) != 1 {
		t.Fatalf("DescribeInstances called %d times, want 1", len(calls))
	}
	if ids := calls[0].InstanceIds; len(ids) != 1 || ids[0] != "i-123" {
		t.Errorf("DescribeInstances asked about %v, want [i-123]", ids)
	}
}

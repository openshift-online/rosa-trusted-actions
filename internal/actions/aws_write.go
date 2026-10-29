package actions

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

func init() {
	RegisterActionFactory(NewAWSRebootAction)
	RegisterActionFactory(NewAWSTerminateAction)
}

// awsWriteResourceInstances is the resource both write actions declare. It
// matches the name aws-get uses for the same objects, so "ec2-instances"
// means one thing across the API whether it is being read or changed.
const awsWriteResourceInstances = "ec2-instances"

// clusterOwnershipTagValue is the tag value marking a resource as belonging to
// one cluster. OpenShift also writes "shared" for resources a cluster uses but
// does not own; a write requires "owned", because shared means something else
// is relying on it too.
const clusterOwnershipTagValue = "owned"

// clusterOwnershipTag is the tag OpenShift puts on the AWS resources it
// creates for a cluster. It is the same tag CAD filters on to find a cluster's
// instances, so both tools agree on what "belongs to this cluster" means.
func clusterOwnershipTag(infraID string) string {
	return "kubernetes.io/cluster/" + infraID
}

// requireClusterInstance resolves the target instance and refuses unless it
// carries the cluster's ownership tag.
//
// This is the check that makes a write action's blast radius the cluster
// rather than its whole AWS account. Credentials obtained for a cluster can
// reach every instance in the account it lives in — in a ROSA account that can
// include other clusters and the customer's own workloads — so reaching an
// instance is not evidence of being entitled to reboot it.
//
// It describes the instance and inspects its tags rather than letting AWS
// filter by tag, so a refusal can distinguish "no such instance" from "that
// instance belongs to someone else". Both are denials; only one of them means
// the caller has the wrong cluster.
func requireClusterInstance(ctx context.Context, clients awsapi.Clients, target AWSTarget) (*ec2types.Instance, error) {
	if target.ID == "" {
		return nil, fmt.Errorf("an instance id is required")
	}
	// The scope resolves this from OCM before authorizing, so an empty one
	// means the lookup produced nothing — a static provider with no infra ID.
	// Refused rather than skipped: an ownership check that cannot run has not
	// passed.
	if target.InfraID == "" {
		return nil, fmt.Errorf("cannot verify that instance %s belongs to the cluster: no infrastructure ID is known for it", target.ID)
	}

	output, err := clients.EC2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{target.ID},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe instance %s in %s: %w", target.ID, target.Region, err)
	}

	// Matched by id rather than taken positionally: the instance that gets
	// written to is the one named in the target, so that has to be the one
	// whose tags were checked.
	var instance *ec2types.Instance
	for _, reservation := range output.Reservations {
		for _, candidate := range reservation.Instances {
			if aws.ToString(candidate.InstanceId) == target.ID {
				instance = &candidate
				break
			}
		}
	}
	if instance == nil {
		return nil, fmt.Errorf("instance %s was not found in %s", target.ID, target.Region)
	}

	tag := clusterOwnershipTag(target.InfraID)
	for _, candidate := range instance.Tags {
		if aws.ToString(candidate.Key) == tag && aws.ToString(candidate.Value) == clusterOwnershipTagValue {
			return instance, nil
		}
	}

	return nil, fmt.Errorf("instance %s does not belong to cluster %s: it is not tagged %s=%s",
		target.ID, target.InfraID, tag, clusterOwnershipTagValue)
}

// AWSRebootAction restarts one of a cluster's EC2 instances.
type AWSRebootAction struct{}

func NewAWSRebootAction() *AWSRebootAction { return &AWSRebootAction{} }

func (a *AWSRebootAction) Name() string { return "aws-reboot" }

func (a *AWSRebootAction) Operation() string { return AWSOperationReboot }

func (a *AWSRebootAction) Resource() string { return awsWriteResourceInstances }

// RequiredServices needs no more than the reads do: the ownership check and
// the reboot are both EC2 calls.
func (a *AWSRebootAction) RequiredServices(AWSTarget) []awsapi.Service {
	return []awsapi.Service{awsapi.ServiceEC2}
}

func (a *AWSRebootAction) Execute(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) (*ActionResult, error) {
	instance, err := requireClusterInstance(ctx, clients, req.Target)
	if err != nil {
		return nil, err
	}

	// RebootInstances is asynchronous and returns nothing: a successful call
	// means AWS accepted the request, not that the instance has restarted. The
	// message says "requested" for that reason.
	if _, err := clients.EC2.RebootInstances(ctx, &ec2.RebootInstancesInput{
		InstanceIds: []string{req.Target.ID},
	}); err != nil {
		return nil, fmt.Errorf("failed to reboot instance %s in %s: %w", req.Target.ID, req.Target.Region, err)
	}

	// The instance as it was found, which records what was acted on — its
	// type, tags and addresses — in the execution's stored output, rather than
	// leaving only an id behind.
	resources, err := awsResourcesFrom([]ec2types.Instance{*instance})
	if err != nil {
		return nil, err
	}

	return &ActionResult{
		Resources: resources,
		Message:   fmt.Sprintf("requested reboot of %s in %s", req.Target.ID, req.Target.Region),
	}, nil
}

// AWSTerminateAction terminates one of a cluster's EC2 instances.
type AWSTerminateAction struct{}

func NewAWSTerminateAction() *AWSTerminateAction { return &AWSTerminateAction{} }

func (a *AWSTerminateAction) Name() string { return "aws-terminate" }

func (a *AWSTerminateAction) Operation() string { return AWSOperationTerminate }

func (a *AWSTerminateAction) Resource() string { return awsWriteResourceInstances }

func (a *AWSTerminateAction) RequiredServices(AWSTarget) []awsapi.Service {
	return []awsapi.Service{awsapi.ServiceEC2}
}

func (a *AWSTerminateAction) Execute(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) (*ActionResult, error) {
	instance, err := requireClusterInstance(ctx, clients, req.Target)
	if err != nil {
		return nil, err
	}

	output, err := clients.EC2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{req.Target.ID},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to terminate instance %s in %s: %w", req.Target.ID, req.Target.Region, err)
	}

	resources, err := awsResourcesFrom([]ec2types.Instance{*instance})
	if err != nil {
		return nil, err
	}

	return &ActionResult{
		Resources: resources,
		Message:   fmt.Sprintf("requested termination of %s in %s%s", req.Target.ID, req.Target.Region, stateChangeSuffix(output)),
	}, nil
}

// stateChangeSuffix reports the transition TerminateInstances returned, when
// it returned one. Unlike reboot, terminate answers with the state the
// instance moved to, which is worth recording: "running -> shutting-down"
// distinguishes a termination that took effect from one issued against an
// instance that was already going away.
func stateChangeSuffix(output *ec2.TerminateInstancesOutput) string {
	if output == nil || len(output.TerminatingInstances) == 0 {
		return ""
	}
	change := output.TerminatingInstances[0]
	if change.PreviousState == nil || change.CurrentState == nil {
		return ""
	}
	return fmt.Sprintf(" (%s -> %s)", change.PreviousState.Name, change.CurrentState.Name)
}

package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/audit"
	"github.com/openshift-online/rosa-trusted-actions/internal/authorization"
	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

// fakeEC2 serves the one read these tests use. The scope's job is routing,
// authorization and client provisioning — what each read does with its client
// is covered in internal/actions — so the remaining EC2API methods exist only
// to satisfy the interface and return nothing.
type fakeEC2 struct {
	instances    []ec2types.Instance
	reboots      int
	terminations int
}

func (f *fakeEC2) DescribeInstances(_ context.Context, _ *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return &ec2.DescribeInstancesOutput{
		Reservations: []ec2types.Reservation{{Instances: f.instances}},
	}, nil
}

func (f *fakeEC2) DescribeSecurityGroups(_ context.Context, _ *ec2.DescribeSecurityGroupsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	return &ec2.DescribeSecurityGroupsOutput{}, nil
}

func (f *fakeEC2) DescribeSubnets(_ context.Context, _ *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	return &ec2.DescribeSubnetsOutput{}, nil
}

func (f *fakeEC2) DescribeRouteTables(_ context.Context, _ *ec2.DescribeRouteTablesInput, _ ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
	return &ec2.DescribeRouteTablesOutput{}, nil
}

func (f *fakeEC2) DescribeVpcs(_ context.Context, _ *ec2.DescribeVpcsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	return &ec2.DescribeVpcsOutput{}, nil
}

func (f *fakeEC2) DescribeDhcpOptions(_ context.Context, _ *ec2.DescribeDhcpOptionsInput, _ ...func(*ec2.Options)) (*ec2.DescribeDhcpOptionsOutput, error) {
	return &ec2.DescribeDhcpOptionsOutput{}, nil
}

// The writes record that they were reached. These tests are about the scope,
// not about what the AWS calls do, so what matters is whether a request that
// should have been stopped earlier ever got this far.
func (f *fakeEC2) RebootInstances(_ context.Context, _ *ec2.RebootInstancesInput, _ ...func(*ec2.Options)) (*ec2.RebootInstancesOutput, error) {
	f.reboots++
	return &ec2.RebootInstancesOutput{}, nil
}

func (f *fakeEC2) TerminateInstances(_ context.Context, _ *ec2.TerminateInstancesInput, _ ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	f.terminations++
	return &ec2.TerminateInstancesOutput{}, nil
}

type fakeAWSProvider struct {
	defaultRegion string
	instances     []ec2types.Instance
	err           error
	// defaultRegionErr stands in for a failed cluster lookup: resolving the
	// default region is a call to OCM now, so it can fail on its own.
	defaultRegionErr error

	// infraID and infraIDErr are the write path's equivalents: the cluster's
	// identity is a second OCM lookup, which can fail or come back empty.
	infraID    string
	infraIDErr error
	// infraIDCluster records which cluster the identity was resolved for, so a
	// test can prove the check is scoped to the cluster that was named.
	infraIDCluster string
	// ec2 is the client handed to the last execution.
	ec2 *fakeEC2

	// requestedRegion and requestedServices record what the scope asked for.
	requestedRegion   string
	requestedServices []awsapi.Service
	// defaultRegionCluster records which cluster the default was resolved for.
	defaultRegionCluster string
}

func (f *fakeAWSProvider) DefaultRegion(_ context.Context, clusterID string) (string, error) {
	f.defaultRegionCluster = clusterID
	if f.defaultRegionErr != nil {
		return "", f.defaultRegionErr
	}
	return f.defaultRegion, nil
}

func (f *fakeAWSProvider) ClusterInfraID(_ context.Context, clusterID string) (string, error) {
	f.infraIDCluster = clusterID
	if f.infraIDErr != nil {
		return "", f.infraIDErr
	}
	return f.infraID, nil
}

func (f *fakeAWSProvider) GetClients(_ context.Context, _, region string, services []awsapi.Service) (awsapi.Clients, error) {
	f.requestedRegion = region
	f.requestedServices = services
	if f.err != nil {
		return awsapi.Clients{}, f.err
	}
	clients := awsapi.Clients{}
	for _, service := range services {
		if service == awsapi.ServiceEC2 {
			// Kept so a test can ask afterwards whether the action reached
			// AWS at all, which is how a refusal is told from a failure.
			f.ec2 = &fakeEC2{instances: f.instances}
			clients.EC2 = f.ec2
		}
	}
	return clients, nil
}

// newTestAWSExecutor builds an executor permitting the given regions and no
// writes — the posture a server has unless someone configures otherwise, which
// is what the read tests should be running against.
func newTestAWSExecutor(regions []string, provider awsapi.ClientProvider) (*Executor, *audit.MockLogger) {
	return newTestAWSPolicyExecutor(authorization.AWSPolicy{AllowedRegions: regions}, provider)
}

// newTestAWSPolicyExecutor spells the whole policy out, for the write tests.
func newTestAWSPolicyExecutor(policy authorization.AWSPolicy, provider awsapi.ClientProvider) (*Executor, *audit.MockLogger) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	auditor := audit.NewMockLogger(logger)
	return New(logger, auditor, NewAWSScope(authorization.NewAWS(logger, policy), provider)), auditor
}

func newTestInstance(id string) ec2types.Instance {
	return ec2types.Instance{InstanceId: &id}
}

func TestAWSScope_HappyPath_List(t *testing.T) {
	provider := &fakeAWSProvider{
		defaultRegion: "us-east-1",
		instances:     []ec2types.Instance{newTestInstance("i-1"), newTestInstance("i-2")},
	}
	exec, auditor := newTestAWSExecutor([]string{"us-east-1"}, provider)

	result := exec.Execute(context.Background(), Request{
		CallerID:  "test-user",
		ClusterID: "cluster-123",
		Action:    actions.NewAWSGetAction(),
		Params:    map[string]string{"resource": "ec2-instances"},
	})

	if !result.Allowed {
		t.Fatalf("expected allowed, got denied: %s", result.Reason)
	}
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(result.Output.Resources) != 2 {
		t.Errorf("expected 2 resources, got %d", len(result.Output.Resources))
	}

	// The scope resolves the provider's default region before authorizing, so
	// both the API call and the audit record name the region actually used.
	if provider.requestedRegion != "us-east-1" {
		t.Errorf("expected region %q, got %q", "us-east-1", provider.requestedRegion)
	}
	if len(auditor.Records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(auditor.Records))
	}
	target, ok := auditor.Records[0].Target.(actions.AWSTarget)
	if !ok {
		t.Fatalf("expected an AWSTarget in the audit record, got %T", auditor.Records[0].Target)
	}
	if target.Region != "us-east-1" {
		t.Errorf("expected audited region %q, got %q", "us-east-1", target.Region)
	}
	if target.Resource != "ec2-instances" {
		t.Errorf("expected audited resource %q, got %q", "ec2-instances", target.Resource)
	}
	if auditor.Records[0].Outcome != audit.OutcomeSuccess {
		t.Errorf("expected outcome %q, got %q", audit.OutcomeSuccess, auditor.Records[0].Outcome)
	}
}

// The region is the cluster's, never the caller's. A caller who sends one is
// refused outright rather than quietly ignored: they have a wrong expectation
// about which account and region they are reading, and succeeding anyway
// would hide that behind a result set they would misread.
func TestAWSScope_CallerSuppliedRegionIsRejected(t *testing.T) {
	provider := &fakeAWSProvider{defaultRegion: "us-east-1"}
	exec, _ := newTestAWSExecutor([]string{"us-east-1"}, provider)

	result := exec.Execute(context.Background(), Request{
		ClusterID: "cluster-123",
		Action:    actions.NewAWSGetAction(),
		Params:    map[string]string{"resource": "ec2-instances", "region": "eu-west-1"},
	})

	if result.Error == nil {
		t.Fatal("expected a caller-supplied region to be rejected, got nil")
	}
	if !strings.Contains(result.Error.Error(), "region") {
		t.Errorf("expected the error to name the offending param, got %q", result.Error)
	}
	// And the region was never honoured on the way to that refusal.
	if provider.requestedRegion != "us-east-1" {
		t.Errorf("expected the cluster's region %q, got %q", "us-east-1", provider.requestedRegion)
	}
}

// The default region is the target cluster's own, so it has to be resolved for
// the cluster the request names — a provider asked about the wrong cluster
// would silently return another cluster's region.
func TestAWSScope_DefaultRegionIsResolvedForTheRequestedCluster(t *testing.T) {
	provider := &fakeAWSProvider{defaultRegion: "us-east-1"}
	exec, _ := newTestAWSExecutor([]string{"us-east-1"}, provider)

	exec.Execute(context.Background(), Request{
		ClusterID: "cluster-123",
		Action:    actions.NewAWSGetAction(),
		Params:    map[string]string{"resource": "ec2-instances"},
	})

	if provider.defaultRegionCluster != "cluster-123" {
		t.Errorf("expected the default region to be resolved for %q, got %q", "cluster-123", provider.defaultRegionCluster)
	}
}

// Resolving the default region is an OCM lookup now, so it can fail before
// authorization has run. That is a failure, not a denial: the caller was never
// refused anything, and reporting it as a denial would say they were.
func TestAWSScope_DefaultRegionFailureIsReportedAsAFailure(t *testing.T) {
	cause := errors.New("no cluster found")
	provider := &fakeAWSProvider{defaultRegionErr: cause}
	exec, auditor := newTestAWSExecutor([]string{"us-east-1"}, provider)

	result := exec.Execute(context.Background(), Request{
		CallerID:  "test-user",
		ClusterID: "cluster-123",
		Action:    actions.NewAWSGetAction(),
		Params:    map[string]string{"resource": "ec2-instances"},
	})

	if result.Error == nil {
		t.Fatal("expected the resolution failure to be reported, got nil")
	}
	if !errors.Is(result.Error, cause) {
		t.Errorf("expected the underlying cause to be wrapped, got %q", result.Error)
	}
	if result.Reason != "" {
		t.Errorf("expected no authorization reason, got %q", result.Reason)
	}

	if len(auditor.Records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(auditor.Records))
	}
	rec := auditor.Records[0]
	if rec.Outcome != audit.OutcomeFailure {
		t.Errorf("expected outcome %q, got %q", audit.OutcomeFailure, rec.Outcome)
	}
	if rec.Decision == audit.DecisionDenied {
		t.Error("expected no denial to be recorded: authorization never ran")
	}
	// The audit trail records the cause, not the caller-facing framing.
	if rec.Error != cause.Error() {
		t.Errorf("expected the audited error to be %q, got %q", cause.Error(), rec.Error)
	}
}

func TestAWSScope_OnlyRequestsDeclaredServices(t *testing.T) {
	provider := &fakeAWSProvider{defaultRegion: "us-east-1"}
	exec, _ := newTestAWSExecutor([]string{"us-east-1"}, provider)

	exec.Execute(context.Background(), Request{
		Action: actions.NewAWSGetAction(),
		Params: map[string]string{"resource": "ec2-instances"},
	})

	if len(provider.requestedServices) != 1 || provider.requestedServices[0] != awsapi.ServiceEC2 {
		t.Errorf("expected only the EC2 client to be requested, got %v", provider.requestedServices)
	}
}

func TestAWSScope_RegionNotInAllowlist(t *testing.T) {
	provider := &fakeAWSProvider{defaultRegion: "us-east-1"}
	exec, auditor := newTestAWSExecutor([]string{"eu-west-1"}, provider)

	// The cluster resolves to us-east-1, which the allowlist does not cover.
	result := exec.Execute(context.Background(), Request{
		Action: actions.NewAWSGetAction(),
		Params: map[string]string{"resource": "ec2-instances"},
	})

	if result.Allowed {
		t.Fatal("expected denied for a region outside the allowlist, got allowed")
	}
	if provider.requestedServices != nil {
		t.Error("expected no AWS clients to be built for a denied request")
	}
	if auditor.Records[0].Outcome != audit.OutcomeSkipped {
		t.Errorf("expected outcome %q, got %q", audit.OutcomeSkipped, auditor.Records[0].Outcome)
	}
}

// A caller omitting the region must not slip past a configured allowlist by
// falling back to whatever default the provider happens to have.
func TestAWSScope_DefaultRegionIsCheckedAgainstAllowlist(t *testing.T) {
	provider := &fakeAWSProvider{defaultRegion: "us-east-1"}
	exec, _ := newTestAWSExecutor([]string{"eu-west-1"}, provider)

	result := exec.Execute(context.Background(), Request{
		Action: actions.NewAWSGetAction(),
		Params: map[string]string{"resource": "ec2-instances"},
	})

	if result.Allowed {
		t.Fatal("expected the provider's default region to be checked against the allowlist")
	}
}

func TestAWSScope_MissingResource(t *testing.T) {
	exec, _ := newTestAWSExecutor([]string{"us-east-1"}, &fakeAWSProvider{defaultRegion: "us-east-1"})

	result := exec.Execute(context.Background(), Request{
		Action: actions.NewAWSGetAction(),
		Params: map[string]string{},
	})

	if result.Allowed {
		t.Fatal("expected denied when no resource is named, got allowed")
	}
}

// The audit trail must record the underlying cause, not the stage framing the
// scope adds for the caller.
func TestAWSScope_ProviderFailure(t *testing.T) {
	provider := &fakeAWSProvider{defaultRegion: "us-east-1", err: fmt.Errorf("credentials unavailable")}
	exec, auditor := newTestAWSExecutor([]string{"us-east-1"}, provider)

	result := exec.Execute(context.Background(), Request{
		Action: actions.NewAWSGetAction(),
		Params: map[string]string{"resource": "ec2-instances"},
	})

	if result.Error == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := result.Error.Error(), "failed to get AWS clients: credentials unavailable"; got != want {
		t.Errorf("expected caller error %q, got %q", want, got)
	}
	if got, want := auditor.Records[0].Error, "credentials unavailable"; got != want {
		t.Errorf("expected audited error %q, got %q", want, got)
	}
}

// A Kubernetes action must never be claimed by the AWS scope, and vice versa;
// this is what keeps AWS requests from being judged by namespace rules.
func TestAWSScope_DoesNotClaimKubeActions(t *testing.T) {
	scope := NewAWSScope(authorization.NewAWS(logrus.New(), authorization.AWSPolicy{}), &fakeAWSProvider{})

	if scope.Supports(actions.NewGetAction()) {
		t.Error("AWS scope must not claim the Kubernetes get action")
	}
	if !scope.Supports(actions.NewAWSGetAction()) {
		t.Error("AWS scope must claim aws-get")
	}
}

func TestKubeScope_DoesNotClaimAWSActions(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	scope := NewKubeScope(authorization.New(logger, nil, nil), &fakeClientProvider{})

	if scope.Supports(actions.NewAWSGetAction()) {
		t.Error("Kubernetes scope must not claim aws-get")
	}
}

// ownedTestInstance is an instance tagged as belonging to the infra ID the
// write tests use, so the action's ownership check passes and the test is
// about the scope rather than about the tag.
func ownedTestInstance(id, infraID string) ec2types.Instance {
	return ec2types.Instance{
		InstanceId: &id,
		Tags: []ec2types.Tag{{
			Key:   aws.String("kubernetes.io/cluster/" + infraID),
			Value: aws.String("owned"),
		}},
	}
}

func writeTestProvider() *fakeAWSProvider {
	return &fakeAWSProvider{
		defaultRegion: "us-east-1",
		infraID:       "my-cluster-abcde",
		instances:     []ec2types.Instance{ownedTestInstance("i-123", "my-cluster-abcde")},
	}
}

// The posture that matters most: a server configured exactly as the previous
// release left it — regions allowed, nothing said about writes — refuses the
// new actions outright. Adding them to the catalog must not grant them.
func TestAWSScope_WriteDeniedByDefaultPolicy(t *testing.T) {
	for _, action := range []actions.Action{actions.NewAWSRebootAction(), actions.NewAWSTerminateAction()} {
		provider := writeTestProvider()
		exec, auditor := newTestAWSExecutor([]string{"us-east-1"}, provider)

		result := exec.Execute(context.Background(), Request{
			ClusterID: "cluster-123",
			Action:    action,
			Params:    map[string]string{"id": "i-123"},
		})

		if result.Allowed {
			t.Errorf("%s: expected denied with no write allowlist configured, got allowed", action.Name())
		}
		// Denied before anything was built, let alone called.
		if provider.ec2 != nil {
			t.Errorf("%s: AWS clients were provisioned for a denied write", action.Name())
		}
		if len(auditor.Records) != 1 || auditor.Records[0].Decision != audit.DecisionDenied {
			t.Errorf("%s: expected one denied audit record, got %+v", action.Name(), auditor.Records)
		}
	}
}

func TestAWSScope_WriteAllowedWhenConfigured(t *testing.T) {
	provider := writeTestProvider()
	exec, auditor := newTestAWSPolicyExecutor(authorization.AWSPolicy{
		AllowedRegions:    []string{"us-east-1"},
		AllowedOperations: []string{"reboot"},
	}, provider)

	result := exec.Execute(context.Background(), Request{
		ClusterID: "cluster-123",
		Action:    actions.NewAWSRebootAction(),
		Params:    map[string]string{"id": "i-123"},
	})

	if !result.Allowed {
		t.Fatalf("expected allowed, got denied: %s", result.Reason)
	}
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if provider.ec2 == nil || provider.ec2.reboots != 1 {
		t.Errorf("expected exactly one reboot to reach AWS, got %+v", provider.ec2)
	}
	if len(auditor.Records) != 1 || auditor.Records[0].Outcome != audit.OutcomeSuccess {
		t.Fatalf("expected one successful audit record, got %+v", auditor.Records)
	}
}

// Enabling reboots must not enable terminations. The allowlist is per
// operation precisely so the reversible one can be switched on alone.
func TestAWSScope_PermittingOneOperationDoesNotPermitAnother(t *testing.T) {
	provider := writeTestProvider()
	exec, _ := newTestAWSPolicyExecutor(authorization.AWSPolicy{
		AllowedRegions:    []string{"us-east-1"},
		AllowedOperations: []string{"reboot"},
	}, provider)

	result := exec.Execute(context.Background(), Request{
		ClusterID: "cluster-123",
		Action:    actions.NewAWSTerminateAction(),
		Params:    map[string]string{"id": "i-123"},
	})

	if result.Allowed {
		t.Error("expected terminate to be denied when only reboot is permitted, got allowed")
	}
	if provider.ec2 != nil && provider.ec2.terminations != 0 {
		t.Errorf("a denied termination still reached AWS %d times", provider.ec2.terminations)
	}
}

// The identity the ownership check uses has to be resolved for the cluster the
// request named. Resolved for the wrong one, the check would compare against
// another cluster's tag — refusing legitimate writes, or, if that cluster
// happened to own the instance, permitting one it should not.
func TestAWSScope_WriteResolvesInfraIDForTheRequestedCluster(t *testing.T) {
	provider := writeTestProvider()
	exec, auditor := newTestAWSPolicyExecutor(authorization.AWSPolicy{
		AllowedRegions:    []string{"us-east-1"},
		AllowedOperations: []string{"reboot"},
	}, provider)

	exec.Execute(context.Background(), Request{
		ClusterID: "cluster-123",
		Action:    actions.NewAWSRebootAction(),
		Params:    map[string]string{"id": "i-123"},
	})

	if provider.infraIDCluster != "cluster-123" {
		t.Errorf("expected the infra ID to be resolved for %q, got %q", "cluster-123", provider.infraIDCluster)
	}
	if len(auditor.Records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(auditor.Records))
	}
	target, ok := auditor.Records[0].Target.(actions.AWSTarget)
	if !ok {
		t.Fatalf("expected an AWSTarget in the audit record, got %T", auditor.Records[0].Target)
	}
	// Audited too: which cluster a write claimed to be acting for is part of
	// what the record has to answer afterwards.
	if target.InfraID != "my-cluster-abcde" {
		t.Errorf("expected the audited infra ID to be %q, got %q", "my-cluster-abcde", target.InfraID)
	}
}

// A cluster whose identity cannot be resolved cannot have its instances
// checked, so the execution stops rather than proceeding unchecked.
func TestAWSScope_WriteFailsWhenInfraIDCannotBeResolved(t *testing.T) {
	provider := writeTestProvider()
	provider.infraIDErr = errors.New("OCM is unreachable")
	exec, _ := newTestAWSPolicyExecutor(authorization.AWSPolicy{
		AllowedRegions:    []string{"us-east-1"},
		AllowedOperations: []string{"reboot"},
	}, provider)

	result := exec.Execute(context.Background(), Request{
		ClusterID: "cluster-123",
		Action:    actions.NewAWSRebootAction(),
		Params:    map[string]string{"id": "i-123"},
	})

	if result.Error == nil {
		t.Fatal("expected an unresolvable infra ID to stop the execution, got nil")
	}
	if provider.ec2 != nil {
		t.Error("AWS clients were provisioned despite the ownership check having nothing to compare against")
	}
}

// A provider that answers with no infra ID at all — the static one, locally —
// must not turn into an unchecked write. The scope lets it through; the
// action refuses it, which is the layer that can say why.
func TestAWSScope_WriteRefusedWhenInfraIDIsEmpty(t *testing.T) {
	provider := writeTestProvider()
	provider.infraID = ""
	exec, _ := newTestAWSPolicyExecutor(authorization.AWSPolicy{
		AllowedRegions:    []string{"us-east-1"},
		AllowedOperations: []string{"reboot"},
	}, provider)

	result := exec.Execute(context.Background(), Request{
		ClusterID: "cluster-123",
		Action:    actions.NewAWSRebootAction(),
		Params:    map[string]string{"id": "i-123"},
	})

	if result.Error == nil {
		t.Fatal("expected a write with no known cluster identity to fail, got nil")
	}
	if provider.ec2.reboots != 0 {
		t.Errorf("rebooted %d instances without verifying ownership", provider.ec2.reboots)
	}
}

// A write action fixes its own resource, so a caller who names a different one
// is refused rather than silently acted upon.
func TestAWSScope_WriteRejectsMismatchedResourceParam(t *testing.T) {
	provider := writeTestProvider()
	exec, _ := newTestAWSPolicyExecutor(authorization.AWSPolicy{
		AllowedRegions:    []string{"us-east-1"},
		AllowedOperations: []string{"reboot"},
	}, provider)

	result := exec.Execute(context.Background(), Request{
		ClusterID: "cluster-123",
		Action:    actions.NewAWSRebootAction(),
		Params:    map[string]string{"id": "i-123", "resource": "load-balancers"},
	})

	if result.Error == nil {
		t.Fatal("expected a mismatched resource param to be rejected, got nil")
	}
	if !strings.Contains(result.Error.Error(), "load-balancers") {
		t.Errorf("expected the error to name the offending resource, got %q", result.Error)
	}
	if provider.ec2 != nil {
		t.Error("AWS clients were provisioned for a rejected request")
	}
}

// Sending the resource the action already acts on is redundant, not wrong.
func TestAWSScope_WriteAcceptsMatchingResourceParam(t *testing.T) {
	provider := writeTestProvider()
	exec, _ := newTestAWSPolicyExecutor(authorization.AWSPolicy{
		AllowedRegions:    []string{"us-east-1"},
		AllowedOperations: []string{"reboot"},
	}, provider)

	result := exec.Execute(context.Background(), Request{
		ClusterID: "cluster-123",
		Action:    actions.NewAWSRebootAction(),
		Params:    map[string]string{"id": "i-123", "resource": "ec2-instances"},
	})

	if !result.Allowed || result.Error != nil {
		t.Fatalf("expected the matching resource to be accepted, got allowed=%v err=%v", result.Allowed, result.Error)
	}
}

// Reads are unaffected by any of this: no second OCM lookup, and no write
// gate standing between them and the region allowlist they already cleared.
func TestAWSScope_ReadDoesNotResolveInfraID(t *testing.T) {
	provider := &fakeAWSProvider{defaultRegion: "us-east-1"}
	exec, _ := newTestAWSExecutor([]string{"us-east-1"}, provider)

	result := exec.Execute(context.Background(), Request{
		ClusterID: "cluster-123",
		Action:    actions.NewAWSGetAction(),
		Params:    map[string]string{"resource": "ec2-instances"},
	})

	if !result.Allowed {
		t.Fatalf("expected the read to stay allowed, got denied: %s", result.Reason)
	}
	if provider.infraIDCluster != "" {
		t.Errorf("a read resolved the cluster's infra ID (for %q), which it has no use for", provider.infraIDCluster)
	}
}

package authorization

import (
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func newTestAWSAuthorizer(regions []string) AWSAuthorizer {
	return NewAWS(logrus.New(), AWSPolicy{AllowedRegions: regions})
}

// newTestAWSWriteAuthorizer is the same with writes configured. Separate so
// the read tests keep asserting against the default posture — no writes
// permitted — rather than quietly gaining them.
func newTestAWSWriteAuthorizer(regions, operations []string) AWSAuthorizer {
	return NewAWS(logrus.New(), AWSPolicy{AllowedRegions: regions, AllowedOperations: operations})
}

// Fail closed. The region allowlist is the only bound on which part of the
// fleet's AWS footprint a read can reach, so losing it must deny everything
// rather than allow everything — otherwise a dropped config turns into the
// widest possible grant at exactly the moment nobody is watching.
func TestAWSAuthorize_DeniedWithoutAllowlist(t *testing.T) {
	authz := newTestAWSAuthorizer(nil)

	result := authz.Authorize(AWSRequest{Region: "us-east-1", Resource: "ec2-instances"})

	if result.Allowed {
		t.Error("expected denied with no allowlist configured, got allowed")
	}
	if result.Reason == "" {
		t.Error("expected a denial reason, got empty string")
	}
}

// Without a resource there is nothing to read, and nothing meaningful to
// record in the audit trail either. The allowlist is populated so this tests
// the resource check rather than passing on the empty-allowlist denial.
func TestAWSAuthorize_ResourceRequired(t *testing.T) {
	authz := newTestAWSAuthorizer([]string{"us-east-1"})

	result := authz.Authorize(AWSRequest{Region: "us-east-1"})

	if result.Allowed {
		t.Error("expected denied when no resource is named, got allowed")
	}
	if result.Reason == "" {
		t.Error("expected a denial reason, got empty string")
	}
}

func TestAWSAuthorize_AllowedRegion(t *testing.T) {
	authz := newTestAWSAuthorizer([]string{"us-east-1", "eu-west-1"})

	result := authz.Authorize(AWSRequest{Region: "eu-west-1", Resource: "ec2-instances"})

	if !result.Allowed {
		t.Errorf("expected allowed, got denied: %s", result.Reason)
	}
}

func TestAWSAuthorize_DeniedRegion(t *testing.T) {
	authz := newTestAWSAuthorizer([]string{"us-east-1"})

	result := authz.Authorize(AWSRequest{Region: "ap-south-1", Resource: "ec2-instances"})

	if result.Allowed {
		t.Error("expected denied for a region outside the allowlist, got allowed")
	}
	if result.Reason == "" {
		t.Error("expected a denial reason, got empty string")
	}
}

// An empty region means "whatever default the server has", which the allowlist
// cannot check — so omitting it must not become a way around the allowlist.
func TestAWSAuthorize_EmptyRegionDeniedWhenAllowlistConfigured(t *testing.T) {
	authz := newTestAWSAuthorizer([]string{"us-east-1"})

	result := authz.Authorize(AWSRequest{Resource: "ec2-instances"})

	if result.Allowed {
		t.Error("expected denied when no region is named and an allowlist is configured, got allowed")
	}
}

// Every deployment that configures nothing gets this list, so what is in it is
// a security property rather than a detail: widening it here would silently
// widen every unconfigured server at once.
func TestDefaultAllowedRegions(t *testing.T) {
	if len(DefaultAllowedRegions) != 1 || DefaultAllowedRegions[0] != "us-east-1" {
		t.Errorf("expected the default allowlist to be [us-east-1], got %v", DefaultAllowedRegions)
	}
}

// And the default has to be one the authorizer actually accepts — a default
// that denied everything would make an unconfigured server simply broken.
func TestAWSAuthorize_DefaultAllowedRegionsAreUsable(t *testing.T) {
	authz := newTestAWSAuthorizer(DefaultAllowedRegions)

	result := authz.Authorize(AWSRequest{Region: "us-east-1", Resource: "ec2-instances"})

	if !result.Allowed {
		t.Errorf("expected the default region to be allowed, got denied: %s", result.Reason)
	}
}

// The write gate's own fail-closed case, and the one that matters most: an
// otherwise perfectly valid request, in an allowed region, is still refused
// because nobody configured this server to perform writes. Reads in the same
// region keep working — that is the whole distinction the gate exists to draw.
func TestAWSAuthorize_WriteDeniedByDefault(t *testing.T) {
	authz := newTestAWSAuthorizer([]string{"us-east-1"})

	write := authz.Authorize(AWSRequest{Region: "us-east-1", Resource: "ec2-instances", Operation: "reboot"})
	if write.Allowed {
		t.Error("expected a write to be denied with no operation allowlist configured, got allowed")
	}
	if write.Reason == "" {
		t.Error("expected a denial reason, got empty string")
	}

	read := authz.Authorize(AWSRequest{Region: "us-east-1", Resource: "ec2-instances"})
	if !read.Allowed {
		t.Errorf("expected a read in the same region to stay allowed, got denied: %s", read.Reason)
	}
}

// And the default constant has to be the thing that produces that posture.
func TestDefaultAllowedOperationsIsEmpty(t *testing.T) {
	if len(DefaultAllowedOperations) != 0 {
		t.Errorf("expected no operations to be permitted by default, got %v", DefaultAllowedOperations)
	}
}

func TestAWSAuthorize_AllowedOperation(t *testing.T) {
	authz := newTestAWSWriteAuthorizer([]string{"us-east-1"}, []string{"reboot"})

	result := authz.Authorize(AWSRequest{Region: "us-east-1", Resource: "ec2-instances", Operation: "reboot"})

	if !result.Allowed {
		t.Errorf("expected allowed, got denied: %s", result.Reason)
	}
}

// Permitting one operation must not permit the others. Enabling reboots is a
// much smaller decision than enabling terminations, and an allowlist that
// could not tell them apart would collapse the two into one.
func TestAWSAuthorize_OtherOperationStillDenied(t *testing.T) {
	authz := newTestAWSWriteAuthorizer([]string{"us-east-1"}, []string{"reboot"})

	result := authz.Authorize(AWSRequest{Region: "us-east-1", Resource: "ec2-instances", Operation: "terminate"})

	if result.Allowed {
		t.Error("expected terminate to be denied when only reboot is permitted, got allowed")
	}
	if !strings.Contains(result.Reason, "terminate") {
		t.Errorf("expected the denial to name the refused operation, got %q", result.Reason)
	}
}

// The two gates are independent, and the region is the broader one: a write
// permitted everywhere is still confined to the allowed regions. The denial
// names the region rather than the operation because that is the one an
// operator has to fix first.
func TestAWSAuthorize_AllowedOperationInDeniedRegion(t *testing.T) {
	authz := newTestAWSWriteAuthorizer([]string{"us-east-1"}, []string{"reboot"})

	result := authz.Authorize(AWSRequest{Region: "ap-south-1", Resource: "ec2-instances", Operation: "reboot"})

	if result.Allowed {
		t.Error("expected denied for a region outside the allowlist, got allowed")
	}
	if !strings.Contains(result.Reason, "ap-south-1") {
		t.Errorf("expected the denial to name the refused region, got %q", result.Reason)
	}
}

// Configuring writes must not loosen the region allowlist's own fail-closed
// behaviour: an empty region list still denies, operations or not.
func TestAWSAuthorize_WriteDeniedWithoutRegionAllowlist(t *testing.T) {
	authz := newTestAWSWriteAuthorizer(nil, []string{"reboot"})

	result := authz.Authorize(AWSRequest{Region: "us-east-1", Resource: "ec2-instances", Operation: "reboot"})

	if result.Allowed {
		t.Error("expected denied with no region allowlist configured, got allowed")
	}
}

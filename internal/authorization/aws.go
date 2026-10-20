package authorization

import "github.com/sirupsen/logrus"

// AWSRequest is what an AWS action is asking to do. It is deliberately
// separate from Request: the Kubernetes rules (namespaces, secret allowlist)
// have no meaning for AWS, and sharing one type would let an AWS request be
// silently judged by them.
type AWSRequest struct {
	Region   string
	Resource string
	// Operation is the mutation the action performs, empty for a read. It is
	// the action's own declaration rather than anything the caller sends, so a
	// request cannot present itself as a read to get past the write allowlist.
	Operation string
}

// DefaultAllowedRegions is the allowlist applied when none is configured. It
// is the single definition of that default: the server reads it through
// internal/config, and action-cli uses it directly, so neither can be pointed
// at a wider set of regions than the other.
//
// It is one region rather than "every region the fleet runs in" deliberately.
// Widening it is a decision someone should have to make explicitly, in server
// configuration, after deciding the reads are safe there.
var DefaultAllowedRegions = []string{"us-east-1"}

// DefaultAllowedOperations is the write allowlist applied when none is
// configured. Unlike DefaultAllowedRegions it is empty, and that is the whole
// point: a server nobody has configured for writes can read AWS but cannot
// change it. Mutating a cluster's cloud infrastructure is not something a
// deployment should acquire by default, so it is switched on by naming the
// operations, one at a time, in server configuration.
var DefaultAllowedOperations = []string{}

// AWSPolicy is the server-side policy an AWS authorizer enforces. The two
// allowlists are a struct rather than two arguments because both are []string:
// passed positionally, transposing them would compile cleanly and silently
// turn the region list into the write list.
type AWSPolicy struct {
	// AllowedRegions bounds which part of the fleet's AWS footprint is
	// reachable at all. Empty denies everything.
	AllowedRegions []string
	// AllowedOperations names the mutations that are permitted. Empty denies
	// every write while still allowing reads.
	AllowedOperations []string
}

type AWSAuthorizer interface {
	Authorize(req AWSRequest) Result
}

type awsAuthorizer struct {
	logger            *logrus.Logger
	allowedRegions    map[string]bool
	allowedOperations map[string]bool
}

var _ AWSAuthorizer = (*awsAuthorizer)(nil)

func NewAWS(logger *logrus.Logger, policy AWSPolicy) AWSAuthorizer {
	return &awsAuthorizer{
		logger:            logger,
		allowedRegions:    stringSet(policy.AllowedRegions),
		allowedOperations: stringSet(policy.AllowedOperations),
	}
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func (a *awsAuthorizer) Authorize(req AWSRequest) Result {
	if req.Resource == "" {
		return Result{Allowed: false, Reason: "resource is required"}
	}

	// No permissive mode, deliberately unlike the Kubernetes authorizer: an
	// unconfigured allowlist denies rather than allows. The region is the only
	// thing bounding which part of the fleet's AWS footprint a read can reach,
	// so losing the policy has to fail closed. Callers get DefaultAllowedRegions
	// when they configure nothing, which means reaching this is a sign the
	// allowlist was emptied on purpose, and an empty allowlist permits nothing.
	if len(a.allowedRegions) == 0 {
		a.logger.Warn("no AWS region allowlist configured — denying all AWS reads")
		return Result{Allowed: false, Reason: "no AWS region allowlist configured"}
	}

	// The region is resolved from the cluster before this runs, so an empty
	// one here means that resolution produced nothing — a static provider with
	// no region configured. Refused rather than passed on, because an empty
	// region is not something the allowlist can check.
	if req.Region == "" {
		return Result{Allowed: false, Reason: "region is required"}
	}

	if !a.allowedRegions[req.Region] {
		a.logger.WithField("region", req.Region).Debug("region not in allowlist")
		return Result{Allowed: false, Reason: "region not in allowlist: " + req.Region}
	}

	// Reads stop here. A write has to clear a second, separate gate, because
	// being allowed to look at a region is not the same as being allowed to
	// change what is in it — and the default server allows the first and not
	// the second.
	//
	// Checked after the region so a request that fails both is refused for the
	// broader reason, which is the one the operator has to fix first.
	if req.Operation != "" && !a.allowedOperations[req.Operation] {
		a.logger.WithFields(logrus.Fields{
			"operation": req.Operation,
			"region":    req.Region,
		}).Warn("AWS write operation is not permitted by the server's allowlist")
		return Result{Allowed: false, Reason: "AWS write operation not permitted: " + req.Operation}
	}

	return Result{Allowed: true, Reason: "authorized"}
}

package actions

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

func init() {
	RegisterActionFactory(NewAWSGetAction)
}

// awsIDSupport says what the target "id" means for a resource type. AWS is not
// uniform about this: most reads narrow to a single object by id, a few have no
// account-wide list form and need a parent id instead, and a few cannot be
// narrowed at all.
type awsIDSupport int

const (
	// awsIDOptional lists every object when no id is given and narrows to one
	// when it is. This is the common case.
	awsIDOptional awsIDSupport = iota
	// awsIDRequired means the AWS call needs a parent identifier — which target
	// group, which hosted zone — and has nothing to return without one.
	awsIDRequired
	// awsIDUnsupported means the AWS call offers no way to narrow to one object.
	awsIDUnsupported
)

// awsTargetParams are the params AWSScope.DeriveTarget consumes to build the
// target. They arrive in the same map as the resource-specific params, so the
// unknown-param check has to let them through.
//
// "region" is deliberately not one of them. The region is always the target
// cluster's own, resolved from OCM rather than supplied by the caller, so a
// "region" param is rejected like any other unrecognised one — a caller who
// sends it has a wrong expectation, and silently ignoring it would hide that.
var awsTargetParams = []string{"resource", "id"}

// awsResourceDescriptor is one AWS resource type aws-get can read. AWS has no
// uniform resource API — every type is a different call on a different service
// client — so each is described here rather than derived from params the way
// the Kubernetes get action derives a GroupVersionResource.
type awsResourceDescriptor struct {
	// service is the only AWS client this read needs, so an execution builds
	// that client and no others.
	service awsapi.Service
	// describe performs the read and converts the result.
	describe func(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error)
	// id is how this resource treats the target id.
	id awsIDSupport
	// idDescription says what the id identifies, for the error raised when a
	// required one is missing. Only meaningful when id is awsIDRequired.
	idDescription string
	// params are the param names this resource accepts on top of
	// awsTargetParams. A param outside the set is rejected rather than ignored:
	// an ignored "filters" would quietly return a wider result set than the
	// caller asked for, and nothing in the response would say so.
	params []string
}

// awsResources is every resource type aws-get can read. Adding one means
// adding an entry here plus its describe function; the list is kept in one
// place so the readable surface stays greppable as it grows.
var awsResources = map[string]awsResourceDescriptor{
	"ec2-instances": {
		service:  awsapi.ServiceEC2,
		describe: describeEC2Instances,
		params:   []string{"filters"},
	},
	"ec2-security-groups": {
		service:  awsapi.ServiceEC2,
		describe: describeEC2SecurityGroups,
		params:   []string{"filters"},
	},
	"ec2-subnets": {
		service:  awsapi.ServiceEC2,
		describe: describeEC2Subnets,
		params:   []string{"filters"},
	},
	"ec2-route-tables": {
		service:  awsapi.ServiceEC2,
		describe: describeEC2RouteTables,
		params:   []string{"filters"},
	},
	"ec2-vpcs": {
		service:  awsapi.ServiceEC2,
		describe: describeEC2Vpcs,
		params:   []string{"filters"},
	},
	"ec2-dhcp-options": {
		service:  awsapi.ServiceEC2,
		describe: describeEC2DhcpOptions,
		params:   []string{"filters"},
	},
	"load-balancers": {
		service:  awsapi.ServiceELBv2,
		describe: describeLoadBalancers,
	},
	"load-balancers-classic": {
		service:  awsapi.ServiceELB,
		describe: describeClassicLoadBalancers,
	},
	"target-groups": {
		service:  awsapi.ServiceELBv2,
		describe: describeTargetGroups,
	},
	"target-health": {
		service:       awsapi.ServiceELBv2,
		describe:      describeTargetHealth,
		id:            awsIDRequired,
		idDescription: "the ARN of the target group whose targets to report on",
	},
	"cloudtrail-events": {
		service:  awsapi.ServiceCloudTrail,
		describe: lookupCloudTrailEvents,
		id:       awsIDUnsupported,
		params:   []string{"filters", "since"},
	},
	"route53-hosted-zones": {
		service:  awsapi.ServiceRoute53,
		describe: describeHostedZones,
		id:       awsIDUnsupported,
	},
	"route53-record-sets": {
		service:       awsapi.ServiceRoute53,
		describe:      describeRecordSets,
		id:            awsIDRequired,
		idDescription: "the ID of the hosted zone whose records to list",
	},
}

type AWSGetAction struct{}

func NewAWSGetAction() *AWSGetAction { return &AWSGetAction{} }

func (a *AWSGetAction) Name() string { return "aws-get" }

func (a *AWSGetAction) RequiredServices(target AWSTarget) []awsapi.Service {
	descriptor, ok := awsResources[target.Resource]
	if !ok {
		// Execute reports the unknown resource; asking for no clients keeps
		// the provider from building any for a request that cannot run.
		return nil
	}
	return []awsapi.Service{descriptor.service}
}

func (a *AWSGetAction) Execute(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) (*ActionResult, error) {
	descriptor, ok := awsResources[req.Target.Resource]
	if !ok {
		return nil, fmt.Errorf("unknown AWS resource %q, must be one of: %s",
			req.Target.Resource, strings.Join(AWSResourceNames(), ", "))
	}

	if err := descriptor.validate(req); err != nil {
		return nil, err
	}

	resources, err := descriptor.describe(ctx, clients, req)
	if err != nil {
		return nil, err
	}

	return &ActionResult{
		Resources: resources,
		Message:   descriptor.message(req, len(resources)),
	}, nil
}

// validate rejects a request the resource cannot serve, before any AWS call is
// made. Input the resource does not understand is refused rather than dropped,
// so a caller never receives a result that silently answers a different
// question from the one they asked.
func (d awsResourceDescriptor) validate(req AWSActionRequest) error {
	switch {
	case d.id == awsIDRequired && req.Target.ID == "":
		return fmt.Errorf("resource %q requires an id: %s", req.Target.Resource, d.idDescription)
	case d.id == awsIDUnsupported && req.Target.ID != "":
		return fmt.Errorf("resource %q cannot be read by id, only listed", req.Target.Resource)
	}

	// Sorted so a request with several unknown params always fails the same
	// way, rather than naming whichever one map iteration reached first.
	names := make([]string, 0, len(req.Params))
	for name, value := range req.Params {
		// A blank param is the same as an absent one: it narrows nothing, so
		// it cannot produce the wider-than-asked-for result this check exists
		// to prevent. Callers that fill every field of a form would otherwise
		// be refused for params they never set.
		if strings.TrimSpace(value) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if slices.Contains(awsTargetParams, name) || slices.Contains(d.params, name) {
			continue
		}
		accepted := "it accepts no params"
		if len(d.params) > 0 {
			accepted = "it accepts: " + strings.Join(d.params, ", ")
		}
		return fmt.Errorf("resource %q does not accept the %q param, %s", req.Target.Resource, name, accepted)
	}
	return nil
}

// message summarizes what was read. Resources with a required id are always
// listings — the id names the parent that was read, not a returned object — so
// reporting them as "got <resource>/<id>" would describe the wrong thing.
func (d awsResourceDescriptor) message(req AWSActionRequest, count int) string {
	switch {
	case d.id == awsIDRequired:
		return fmt.Sprintf("listed %d %s for %s in %s", count, req.Target.Resource, req.Target.ID, req.Target.Region)
	case req.Target.ID != "":
		return fmt.Sprintf("got %s/%s in %s", req.Target.Resource, req.Target.ID, req.Target.Region)
	default:
		return fmt.Sprintf("listed %d %s in %s", count, req.Target.Resource, req.Target.Region)
	}
}

// AWSResourceNames lists every resource type aws-get can read, sorted. It is
// exported so the catalog can describe the resource param from the registry
// itself rather than from a copy that drifts as resources are added.
func AWSResourceNames() []string {
	names := make([]string, 0, len(awsResources))
	for name := range awsResources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// awsFilter is one caller-supplied filter: an AWS API filter name and the
// values it accepts.
type awsFilter struct {
	Name   string
	Values []string
}

// parseAWSFilters reads the "filters" param: comma-separated Name=Value pairs,
// where repeating a name adds another accepted value. Because the separators
// are fixed, a filter value cannot itself contain a comma or an equals sign.
// The result is ordered by name so the same params always produce the same
// API call.
func parseAWSFilters(raw string) ([]awsFilter, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	values := map[string][]string{}
	names := []string{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		name, value, found := strings.Cut(pair, "=")
		name = strings.TrimSpace(name)
		if !found || name == "" {
			return nil, fmt.Errorf("invalid filter %q: expected Name=Value", pair)
		}
		if _, seen := values[name]; !seen {
			names = append(names, name)
		}
		values[name] = append(values[name], strings.TrimSpace(value))
	}

	sort.Strings(names)
	filters := make([]awsFilter, 0, len(names))
	for _, name := range names {
		filters = append(filters, awsFilter{Name: name, Values: values[name]})
	}
	return filters, nil
}

package actions

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

// describeHostedZones reads the account's Route 53 hosted zones. Hosted zones
// are a global resource, so the target region only decides which endpoint is
// called, not what comes back.
func describeHostedZones(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error) {
	zones := []route53types.HostedZone{}
	paginator := route53.NewListHostedZonesPaginator(clients.Route53, &route53.ListHostedZonesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list Route 53 hosted zones: %w", err)
		}
		zones = append(zones, page.HostedZones...)
	}

	return awsResourcesFrom(zones)
}

// describeRecordSets reads the records in one hosted zone, named by the target
// id — there is no call that lists records across zones, which is why the
// descriptor marks the id required.
//
// The SDK ships no paginator for this call: Route 53 resumes from the record
// name, type and identifier to continue at rather than an opaque token, so the
// loop is written out here.
func describeRecordSets(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error) {
	input := &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(req.Target.ID),
	}

	records := []route53types.ResourceRecordSet{}
	for {
		out, err := clients.Route53.ListResourceRecordSets(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to list Route 53 record sets in zone %s: %w", req.Target.ID, err)
		}
		records = append(records, out.ResourceRecordSets...)

		if !out.IsTruncated {
			break
		}
		input.StartRecordName = out.NextRecordName
		input.StartRecordType = out.NextRecordType
		input.StartRecordIdentifier = out.NextRecordIdentifier
	}

	return awsResourcesFrom(records)
}

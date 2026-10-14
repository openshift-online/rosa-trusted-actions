package actions

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cloudtrailtypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

// cloudTrailLookbackDefault bounds a lookup that names no window. CloudTrail
// keeps 90 days of events, and paginating all of them in a busy account can
// take a very long time, so a request that does not say how far back to look
// gets a short window instead of the full retention period. A caller who needs
// more says so with the "since" param.
const cloudTrailLookbackDefault = 2 * time.Hour

// CloudTrailLookbackMax bounds how far back a caller may ask, whatever the
// retention period allows. The cost of a window is not the duration but the
// number of events in it: LookupEvents is heavily throttled and returns a
// small page at a time, every page is accumulated in memory here, and the
// whole set is stored as one result. A wide window in a busy account is
// therefore slow and large at once.
//
// Rejected up front rather than left to the worker's execution timeout. That
// timeout is a backstop, not a bound: it spends a worker's entire budget and
// then returns nothing, whereas a caller who asked for too much can be told
// immediately and narrow the window. Seven days is well beyond the default
// while staying a window that can plausibly be paginated.
const CloudTrailLookbackMax = 7 * 24 * time.Hour

// lookupCloudTrailEvents reads the CloudTrail event history — who called which
// API, when. The window is always bounded, never open-ended.
func lookupCloudTrailEvents(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) ([]map[string]interface{}, error) {
	lookback := cloudTrailLookbackDefault
	if raw := strings.TrimSpace(req.Params["since"]); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid since %q: expected a duration such as 30m, 2h or 48h: %w", raw, err)
		}
		if parsed <= 0 {
			return nil, fmt.Errorf("invalid since %q: must be positive", raw)
		}
		if parsed > CloudTrailLookbackMax {
			return nil, fmt.Errorf("invalid since %q: the longest supported lookback is %s", raw, CloudTrailLookbackMax)
		}
		lookback = parsed
	}

	attributes, err := cloudTrailAttributes(req.Params["filters"])
	if err != nil {
		return nil, err
	}

	input := &cloudtrail.LookupEventsInput{
		StartTime:        aws.Time(time.Now().UTC().Add(-lookback)),
		LookupAttributes: attributes,
	}

	events := []cloudtrailtypes.Event{}
	paginator := cloudtrail.NewLookupEventsPaginator(clients.CloudTrail, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to look up CloudTrail events in %s: %w", req.Target.Region, err)
		}
		events = append(events, page.Events...)
	}

	return awsResourcesFrom(events)
}

// cloudTrailAttributes converts the "filters" param into CloudTrail's lookup
// attributes. CloudTrail is much stricter than EC2: it takes at most one
// attribute, its key must come from a fixed set, and it carries a single
// value. Each of those is checked here so the caller gets a message naming the
// problem rather than an opaque rejection from the service.
func cloudTrailAttributes(raw string) ([]cloudtrailtypes.LookupAttribute, error) {
	filters, err := parseAWSFilters(raw)
	if err != nil {
		return nil, err
	}
	if len(filters) == 0 {
		return nil, nil
	}
	if len(filters) > 1 {
		return nil, fmt.Errorf("cloudtrail-events accepts a single filter, got %d", len(filters))
	}

	filter := filters[0]
	if len(filter.Values) != 1 {
		return nil, fmt.Errorf("cloudtrail-events filter %q accepts a single value, got %d", filter.Name, len(filter.Values))
	}

	key := cloudtrailtypes.LookupAttributeKey(filter.Name)
	if !slices.Contains(key.Values(), key) {
		return nil, fmt.Errorf("unknown cloudtrail-events filter %q, must be one of: %s",
			filter.Name, strings.Join(cloudTrailAttributeKeys(), ", "))
	}

	return []cloudtrailtypes.LookupAttribute{{
		AttributeKey:   key,
		AttributeValue: aws.String(filter.Values[0]),
	}}, nil
}

func cloudTrailAttributeKeys() []string {
	keys := cloudtrailtypes.LookupAttributeKey("").Values()
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		names = append(names, string(key))
	}
	slices.Sort(names)
	return names
}

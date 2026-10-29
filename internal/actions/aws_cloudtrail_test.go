package actions

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cloudtrailtypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

// fakeCloudTrailAPI serves LookupEvents from pages of event IDs.
type fakeCloudTrailAPI struct {
	err   error
	pages [][]string
	calls []*cloudtrail.LookupEventsInput
}

func (f *fakeCloudTrailAPI) LookupEvents(_ context.Context, in *cloudtrail.LookupEventsInput, _ ...func(*cloudtrail.Options)) (*cloudtrail.LookupEventsOutput, error) {
	// The paginator mutates the input between pages, so the call is recorded
	// by value — otherwise every recorded call would show the last token.
	recorded := *in
	f.calls = append(f.calls, &recorded)
	if f.err != nil {
		return nil, f.err
	}

	index := len(f.calls) - 1
	if index >= len(f.pages) {
		return &cloudtrail.LookupEventsOutput{}, nil
	}

	events := make([]cloudtrailtypes.Event, 0, len(f.pages[index]))
	for _, id := range f.pages[index] {
		events = append(events, cloudtrailtypes.Event{EventId: aws.String(id)})
	}

	out := &cloudtrail.LookupEventsOutput{Events: events}
	if index+1 < len(f.pages) {
		out.NextToken = aws.String(fmt.Sprintf("page-%d", index+1))
	}
	return out, nil
}

// assertLookback checks how far back the request reached. The window is
// measured from the moment of the call, which is some way after the test read
// the clock, so the start time can only be pinned to a range: it is at or
// after "before minus the window", and not much later than that.
func assertLookback(t *testing.T, start *time.Time, before time.Time, want time.Duration) {
	t.Helper()
	if start == nil {
		t.Fatal("expected a StartTime, got nil")
	}

	drift := start.Sub(before.Add(-want))
	if drift < 0 || drift > time.Minute {
		t.Errorf("expected a lookback of about %v, got %v", want, before.Sub(*start))
	}
}

func cloudTrailRequest(params map[string]string) AWSActionRequest {
	return AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "cloudtrail-events"},
		Params: params,
	}
}

func TestLookupCloudTrailEvents_FlattensPages(t *testing.T) {
	client := &fakeCloudTrailAPI{pages: [][]string{{"e-1", "e-2"}, {"e-3"}}}

	resources, err := lookupCloudTrailEvents(context.Background(), awsapi.Clients{CloudTrail: client}, cloudTrailRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := resourceIDs(t, "EventId", resources), []string{"e-1", "e-2", "e-3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
	if len(client.calls) != 2 {
		t.Errorf("expected 2 paginated calls, got %d", len(client.calls))
	}
}

func TestLookupCloudTrailEvents_Empty(t *testing.T) {
	resources, err := lookupCloudTrailEvents(context.Background(), awsapi.Clients{CloudTrail: &fakeCloudTrailAPI{}}, cloudTrailRequest(nil))
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

// A lookup with no window must still be bounded — an open-ended LookupEvents
// walks CloudTrail's full 90-day retention.
func TestLookupCloudTrailEvents_DefaultsToABoundedWindow(t *testing.T) {
	client := &fakeCloudTrailAPI{pages: [][]string{{"e-1"}}}
	before := time.Now().UTC()

	if _, err := lookupCloudTrailEvents(context.Background(), awsapi.Clients{CloudTrail: client}, cloudTrailRequest(nil)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertLookback(t, client.calls[0].StartTime, before, cloudTrailLookbackDefault)
}

func TestLookupCloudTrailEvents_SinceWidensTheWindow(t *testing.T) {
	client := &fakeCloudTrailAPI{pages: [][]string{{"e-1"}}}
	before := time.Now().UTC()

	if _, err := lookupCloudTrailEvents(context.Background(), awsapi.Clients{CloudTrail: client},
		cloudTrailRequest(map[string]string{"since": "48h"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertLookback(t, client.calls[0].StartTime, before, 48*time.Hour)
}

func TestLookupCloudTrailEvents_InvalidSince(t *testing.T) {
	tests := []struct {
		name  string
		since string
	}{
		{name: "not a duration", since: "yesterday"},
		{name: "a bare number", since: "24"},
		{name: "zero", since: "0s"},
		{name: "negative", since: "-2h"},
		// The full 90-day retention: accepted before the cap existed, and the
		// case the cap is for.
		{name: "the whole retention period", since: "2160h"},
		{name: "just over the maximum", since: "169h"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeCloudTrailAPI{pages: [][]string{{"e-1"}}}

			_, err := lookupCloudTrailEvents(context.Background(), awsapi.Clients{CloudTrail: client},
				cloudTrailRequest(map[string]string{"since": tc.since}))

			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.since) {
				t.Errorf("expected the error to quote the value, got %q", err.Error())
			}
			if len(client.calls) != 0 {
				t.Errorf("expected no API calls for an invalid window, got %d", len(client.calls))
			}
		})
	}
}

// The boundary is inclusive, and the cap is a real ceiling rather than a
// silent clamp: asking for the maximum gets the maximum.
func TestLookupCloudTrailEvents_MaximumSinceIsAccepted(t *testing.T) {
	client := &fakeCloudTrailAPI{pages: [][]string{{"e-1"}}}
	before := time.Now().UTC()

	if _, err := lookupCloudTrailEvents(context.Background(), awsapi.Clients{CloudTrail: client},
		cloudTrailRequest(map[string]string{"since": CloudTrailLookbackMax.String()})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertLookback(t, client.calls[0].StartTime, before, CloudTrailLookbackMax)
}

// The refusal has to say what the limit is. A caller who is told only that
// their window is too wide has no way to pick a narrower one but to guess.
func TestLookupCloudTrailEvents_OverLongSinceNamesTheLimit(t *testing.T) {
	client := &fakeCloudTrailAPI{pages: [][]string{{"e-1"}}}

	_, err := lookupCloudTrailEvents(context.Background(), awsapi.Clients{CloudTrail: client},
		cloudTrailRequest(map[string]string{"since": "2160h"}))

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), CloudTrailLookbackMax.String()) {
		t.Errorf("expected the error to name the %s limit, got %q", CloudTrailLookbackMax, err.Error())
	}
}

// A blank since is not an error — it just means the caller did not set one.
func TestLookupCloudTrailEvents_BlankSinceUsesTheDefault(t *testing.T) {
	client := &fakeCloudTrailAPI{pages: [][]string{{"e-1"}}}
	before := time.Now().UTC()

	if _, err := lookupCloudTrailEvents(context.Background(), awsapi.Clients{CloudTrail: client},
		cloudTrailRequest(map[string]string{"since": "  "})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertLookback(t, client.calls[0].StartTime, before, cloudTrailLookbackDefault)
}

func TestLookupCloudTrailEvents_FilterIsPushedToTheAPI(t *testing.T) {
	client := &fakeCloudTrailAPI{pages: [][]string{{"e-1"}}}

	if _, err := lookupCloudTrailEvents(context.Background(), awsapi.Clients{CloudTrail: client},
		cloudTrailRequest(map[string]string{"filters": "EventName=TerminateInstances"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	attributes := client.calls[0].LookupAttributes
	if len(attributes) != 1 {
		t.Fatalf("expected 1 lookup attribute, got %d", len(attributes))
	}
	if attributes[0].AttributeKey != cloudtrailtypes.LookupAttributeKeyEventName {
		t.Errorf("expected key EventName, got %q", attributes[0].AttributeKey)
	}
	if got := aws.ToString(attributes[0].AttributeValue); got != "TerminateInstances" {
		t.Errorf("expected value TerminateInstances, got %q", got)
	}
}

func TestLookupCloudTrailEvents_NoFilterSendsNoAttributes(t *testing.T) {
	client := &fakeCloudTrailAPI{pages: [][]string{{"e-1"}}}

	if _, err := lookupCloudTrailEvents(context.Background(), awsapi.Clients{CloudTrail: client}, cloudTrailRequest(nil)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.calls[0].LookupAttributes != nil {
		t.Errorf("expected no lookup attributes, got %v", client.calls[0].LookupAttributes)
	}
}

// CloudTrail's filtering is far narrower than EC2's, and each limit is checked
// here rather than left to the service: a rejection from AWS would reach the
// caller as an opaque validation error naming neither the resource nor the
// param they set.
func TestLookupCloudTrailEvents_RejectsUnsupportedFilters(t *testing.T) {
	tests := []struct {
		name    string
		filters string
		wants   string
	}{
		{
			name:    "more than one attribute",
			filters: "EventName=StopInstances,Username=alice",
			wants:   "single filter",
		},
		{
			name:    "more than one value",
			filters: "EventName=StopInstances,EventName=TerminateInstances",
			wants:   "single value",
		},
		{
			name:    "a key CloudTrail does not have",
			filters: "InstanceId=i-1",
			wants:   "unknown cloudtrail-events filter",
		},
		{
			name:    "malformed",
			filters: "EventName",
			wants:   "expected Name=Value",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeCloudTrailAPI{pages: [][]string{{"e-1"}}}

			_, err := lookupCloudTrailEvents(context.Background(), awsapi.Clients{CloudTrail: client},
				cloudTrailRequest(map[string]string{"filters": tc.filters}))

			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("expected the error to mention %q, got %q", tc.wants, err.Error())
			}
			if len(client.calls) != 0 {
				t.Errorf("expected no API calls for an invalid filter, got %d", len(client.calls))
			}
		})
	}
}

// The unknown-key error is only useful if it says what the valid keys are.
func TestLookupCloudTrailEvents_UnknownFilterListsTheValidKeys(t *testing.T) {
	_, err := lookupCloudTrailEvents(context.Background(), awsapi.Clients{CloudTrail: &fakeCloudTrailAPI{}},
		cloudTrailRequest(map[string]string{"filters": "InstanceId=i-1"}))

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	for _, key := range []string{"EventName", "Username", "EventSource"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("expected the error to list %q, got %q", key, err.Error())
		}
	}
}

func TestLookupCloudTrailEvents_APIError(t *testing.T) {
	_, err := lookupCloudTrailEvents(context.Background(),
		awsapi.Clients{CloudTrail: &fakeCloudTrailAPI{err: fmt.Errorf("AccessDenied")}}, AWSActionRequest{
			Target: AWSTarget{Region: "eu-west-1", Resource: "cloudtrail-events"},
		})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := err.Error(), "failed to look up CloudTrail events in eu-west-1: AccessDenied"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

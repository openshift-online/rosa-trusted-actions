package actions

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"

	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
)

// fakeRoute53API serves both Route 53 reads. Hosted zones paginate by an
// opaque marker; record sets resume from the name and type to continue at, so
// the fake records the whole input to prove the loop carries them forward.
type fakeRoute53API struct {
	err error

	zonePages [][]string
	zoneCalls []*route53.ListHostedZonesInput

	recordPages [][]string
	recordCalls []route53.ListResourceRecordSetsInput
}

func (f *fakeRoute53API) ListHostedZones(_ context.Context, in *route53.ListHostedZonesInput, _ ...func(*route53.Options)) (*route53.ListHostedZonesOutput, error) {
	f.zoneCalls = append(f.zoneCalls, in)
	if f.err != nil {
		return nil, f.err
	}

	index := len(f.zoneCalls) - 1
	if index >= len(f.zonePages) {
		return &route53.ListHostedZonesOutput{}, nil
	}

	zones := make([]route53types.HostedZone, 0, len(f.zonePages[index]))
	for _, id := range f.zonePages[index] {
		zones = append(zones, route53types.HostedZone{Id: aws.String(id)})
	}

	out := &route53.ListHostedZonesOutput{HostedZones: zones}
	if index+1 < len(f.zonePages) {
		out.IsTruncated = true
		out.NextMarker = aws.String(fmt.Sprintf("page-%d", index+1))
	}
	return out, nil
}

func (f *fakeRoute53API) ListResourceRecordSets(_ context.Context, in *route53.ListResourceRecordSetsInput, _ ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error) {
	// Recorded by value: the loop reuses one input struct across pages, so a
	// pointer would leave every recorded call showing the final cursor.
	f.recordCalls = append(f.recordCalls, *in)
	if f.err != nil {
		return nil, f.err
	}

	index := len(f.recordCalls) - 1
	if index >= len(f.recordPages) {
		return &route53.ListResourceRecordSetsOutput{}, nil
	}

	records := make([]route53types.ResourceRecordSet, 0, len(f.recordPages[index]))
	for _, name := range f.recordPages[index] {
		records = append(records, route53types.ResourceRecordSet{
			Name: aws.String(name),
			Type: route53types.RRTypeA,
		})
	}

	out := &route53.ListResourceRecordSetsOutput{ResourceRecordSets: records}
	if index+1 < len(f.recordPages) {
		out.IsTruncated = true
		out.NextRecordName = aws.String(f.recordPages[index+1][0])
		out.NextRecordType = route53types.RRTypeCname
		out.NextRecordIdentifier = aws.String(fmt.Sprintf("id-%d", index+1))
	}
	return out, nil
}

func TestDescribeHostedZones_FlattensPages(t *testing.T) {
	client := &fakeRoute53API{zonePages: [][]string{{"Z1", "Z2"}, {"Z3"}}}

	resources, err := describeHostedZones(context.Background(), awsapi.Clients{Route53: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "route53-hosted-zones"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := resourceIDs(t, "Id", resources), []string{"Z1", "Z2", "Z3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
	if len(client.zoneCalls) != 2 {
		t.Errorf("expected 2 paginated calls, got %d", len(client.zoneCalls))
	}
}

func TestDescribeHostedZones_Empty(t *testing.T) {
	resources, err := describeHostedZones(context.Background(), awsapi.Clients{Route53: &fakeRoute53API{}}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "route53-hosted-zones"},
	})
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

// Hosted zones are global, so the error deliberately does not name a region —
// saying "in us-east-1" would suggest another region might answer differently.
func TestDescribeHostedZones_APIError(t *testing.T) {
	_, err := describeHostedZones(context.Background(),
		awsapi.Clients{Route53: &fakeRoute53API{err: fmt.Errorf("AccessDenied")}}, AWSActionRequest{
			Target: AWSTarget{Region: "eu-west-1", Resource: "route53-hosted-zones"},
		})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := err.Error(), "failed to list Route 53 hosted zones: AccessDenied"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestDescribeRecordSets_ReadsTheNamedZone(t *testing.T) {
	client := &fakeRoute53API{recordPages: [][]string{{"a.example.com."}}}

	resources, err := describeRecordSets(context.Background(), awsapi.Clients{Route53: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "route53-record-sets", ID: "Z1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(client.recordCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(client.recordCalls))
	}
	if got := aws.ToString(client.recordCalls[0].HostedZoneId); got != "Z1" {
		t.Errorf("expected HostedZoneId Z1, got %q", got)
	}
	if got, want := resourceIDs(t, "Name", resources), []string{"a.example.com."}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

// Route 53 pages record sets by the name, type and identifier to resume from
// rather than an opaque token, and the loop is hand-written because the SDK
// ships no paginator for it — so the cursor being carried forward correctly is
// the thing most worth pinning down.
func TestDescribeRecordSets_CarriesTheCursorForward(t *testing.T) {
	client := &fakeRoute53API{recordPages: [][]string{
		{"a.example.com."},
		{"b.example.com."},
		{"c.example.com."},
	}}

	resources, err := describeRecordSets(context.Background(), awsapi.Clients{Route53: client}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "route53-record-sets", ID: "Z1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"a.example.com.", "b.example.com.", "c.example.com."}
	if got := resourceIDs(t, "Name", resources); !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
	if len(client.recordCalls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(client.recordCalls))
	}

	// The first call starts at the beginning; each later one resumes from what
	// the previous response said to continue at.
	if client.recordCalls[0].StartRecordName != nil {
		t.Errorf("expected the first call to start at the beginning, got %q", *client.recordCalls[0].StartRecordName)
	}
	for i, wantName := range []string{"b.example.com.", "c.example.com."} {
		call := client.recordCalls[i+1]
		if got := aws.ToString(call.StartRecordName); got != wantName {
			t.Errorf("call %d: expected StartRecordName %q, got %q", i+1, wantName, got)
		}
		if call.StartRecordType != route53types.RRTypeCname {
			t.Errorf("call %d: expected StartRecordType CNAME, got %q", i+1, call.StartRecordType)
		}
		if got, want := aws.ToString(call.StartRecordIdentifier), fmt.Sprintf("id-%d", i+1); got != want {
			t.Errorf("call %d: expected StartRecordIdentifier %q, got %q", i+1, want, got)
		}
		// Every page must stay pinned to the same zone.
		if got := aws.ToString(call.HostedZoneId); got != "Z1" {
			t.Errorf("call %d: expected HostedZoneId Z1, got %q", i+1, got)
		}
	}
}

func TestDescribeRecordSets_Empty(t *testing.T) {
	resources, err := describeRecordSets(context.Background(), awsapi.Clients{Route53: &fakeRoute53API{}}, AWSActionRequest{
		Target: AWSTarget{Region: "us-east-1", Resource: "route53-record-sets", ID: "Z1"},
	})
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

func TestDescribeRecordSets_APIError(t *testing.T) {
	_, err := describeRecordSets(context.Background(),
		awsapi.Clients{Route53: &fakeRoute53API{err: fmt.Errorf("NoSuchHostedZone")}}, AWSActionRequest{
			Target: AWSTarget{Region: "eu-west-1", Resource: "route53-record-sets", ID: "Z1"},
		})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := err.Error(), "failed to list Route 53 record sets in zone Z1: NoSuchHostedZone"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

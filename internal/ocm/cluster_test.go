package ocm

import (
	"context"
	"strings"
	"testing"
)

// The identifier is interpolated into an OCM search expression, so what the
// regex accepts is a security boundary rather than a convenience check.
func TestClusterIdentifier_Accepted(t *testing.T) {
	valid := []string{
		"2a1b3c4d5e6f7g8h9i0j",                 // internal ID
		"01234567-89ab-cdef-0123-456789abcdef", // external ID (UUID)
		"my-cluster-01",                        // display name
		"a",
		strings.Repeat("a", 64),
	}
	for _, identifier := range valid {
		if !clusterIdentifierRe.MatchString(identifier) {
			t.Errorf("expected %q to be accepted", identifier)
		}
	}
}

func TestClusterIdentifier_Rejected(t *testing.T) {
	invalid := []string{
		"",
		strings.Repeat("a", 65),
		"cluster' or '1'='1",   // the injection this guard exists for
		"cluster') or true--",  //
		"my cluster",           // space
		"cluster\nname",        // newline
		"cluster'",             // a lone quote is enough to break out
		"clusters/../accounts", // path traversal in a search term
	}
	for _, identifier := range invalid {
		if clusterIdentifierRe.MatchString(identifier) {
			t.Errorf("expected %q to be rejected", identifier)
		}
	}
}

// Get must validate before it touches the connection. This client has none, so
// reaching the lookup at all would panic — which is exactly what makes this a
// meaningful check that the guard runs first.
func TestClusters_GetRejectsBeforeQuerying(t *testing.T) {
	service := clusters{client: &Client{}}

	_, err := service.Get(context.Background(), "cluster' or '1'='1")
	if err == nil {
		t.Fatal("expected an invalid identifier to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "invalid cluster identifier") {
		t.Errorf("expected the error to name the invalid identifier, got %q", err.Error())
	}
}

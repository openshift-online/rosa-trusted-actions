package ocm

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	sdkClient "github.com/openshift-online/ocm-sdk-go"
	cmv1 "github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1"
)

// clusterIdentifierRe constrains a cluster identifier to the characters shared
// by every form OCM accepts for a cluster lookup: the internal ID
// (alphanumeric), the external ID (an RFC4122 UUID, so hex plus dashes) and the
// display name. It validates the character class rather than the shape of
// either format, and exists to stop a caller-supplied identifier from being
// interpolated into the search expression below as anything but a literal.
var clusterIdentifierRe = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)

// Clusters looks clusters up in OCM. It is an interface so callers that only
// need a cluster — the AWS client provider, which resolves a cluster to the
// account its credentials come from — can be tested without a live connection.
type Clusters interface {
	Get(ctx context.Context, identifier string) (*cmv1.Cluster, error)
}

type clusters service

var _ Clusters = &clusters{}

// Get resolves a cluster identifier to the cluster itself. The identifier may
// be the internal ID, the external ID or the display name, matching what an SRE
// would paste; an identifier matching more than one cluster is an error rather
// than a guess.
func (c clusters) Get(ctx context.Context, identifier string) (*cmv1.Cluster, error) {
	if !clusterIdentifierRe.MatchString(identifier) {
		return nil, fmt.Errorf("invalid cluster identifier: %q", identifier)
	}

	con, err := c.client.sdkConn()
	if err != nil {
		return nil, err
	}

	// '=' rather than 'like': no wildcard matching is intended here.
	search := fmt.Sprintf("(id = '%[1]s' or external_id = '%[1]s' or display_name = '%[1]s')", identifier)
	response, err := con.ClustersMgmt().V1().Clusters().List().
		Search(search).
		Size(2). // Only enough to detect an ambiguous identifier.
		SendContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to look up cluster %q in OCM: %w", identifier, err)
	}
	if response.Status() != http.StatusOK {
		return nil, fmt.Errorf("failed to look up cluster %q in OCM: status %d", identifier, response.Status())
	}
	if response.Total() > 1 {
		return nil, fmt.Errorf("cluster identifier %q is ambiguous: it matches %d clusters", identifier, response.Total())
	}
	if response.Total() == 0 {
		return nil, fmt.Errorf("no cluster found for %q", identifier)
	}

	return response.Items().Get(0), nil
}

// Connection exposes the underlying OCM connection. backplane-cli's cloud
// credential flow takes a connection rather than a token, because it refreshes
// the token itself over the life of the call.
//
// It can fail because Client holds its connection behind an interface so tests
// can substitute a fake. A fake has no concrete SDK connection to hand out, and
// backplane-cli accepts nothing else — so that case is an error rather than a
// nil the caller would only discover when the credential call dereferenced it.
func (c *Client) Connection() (*sdkClient.Connection, error) { return c.sdkConn() }

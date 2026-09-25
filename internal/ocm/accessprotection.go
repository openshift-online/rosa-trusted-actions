package ocm

import (
	"context"
	"fmt"

	acctrspv1 "github.com/openshift-online/ocm-sdk-go/accesstransparency/v1"
)

type AccessProtection interface {
	IsAccessProtectionEnabled(ctx context.Context, clusterID string) (enabled bool, err error)
	GetClusterActiveAccessRequest(ctx context.Context, clusterID string) (*acctrspv1.AccessRequest, error)
}

type accessProtection service

var _ AccessProtection = &accessProtection{}

func (a accessProtection) IsAccessProtectionEnabled(ctx context.Context, clusterID string) (enabled bool, err error) {
	getResponse, err := a.client.connection.AccessTransparency().V1().AccessProtection().Get().ClusterId(clusterID).SendContext(ctx)

	if getResponse == nil || err != nil {
		return false, fmt.Errorf("failed to GET /api/access_transparency/v1/access_protection endpoint: %w", err)
	}

	body := getResponse.Body()

	if body == nil {
		return false, fmt.Errorf("no body in GET response on the /api/access_transparency/v1/access_protection endpoint")
	}

	return body.Enabled(), nil
}

func (a accessProtection) GetClusterActiveAccessRequest(ctx context.Context, clusterID string) (*acctrspv1.AccessRequest, error) {
	// Cluster is access protected, checking if the access request has been approved
	search := fmt.Sprintf("cluster_id = '%s' and (status.state = 'Pending' or status.state = 'Approved')", clusterID)
	listResponse, err := a.client.connection.AccessTransparency().V1().AccessRequests().List().Search(search).SendContext(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to GET /api/access_transparency/v1/access_requests endpoint: %w", err)
	}

	accessRequests := listResponse.Items()

	if accessRequests == nil || accessRequests.Empty() || accessRequests.Get(0) == nil {
		return nil, nil
	}

	return accessRequests.Get(0), nil
}

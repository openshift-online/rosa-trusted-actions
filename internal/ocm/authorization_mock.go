package ocm

import (
	"context"
	"slices"
)

// authorizationMock returns allowed=true for every request
type authorizationMock service

var _ Authorization = &authorizationMock{}

func (a authorizationMock) SelfAccessReview(ctx context.Context, action, resourceType, organizationID, subscriptionID, clusterID string) (allowed bool, err error) {
	return true, nil
}

func (a authorizationMock) AccessReview(ctx context.Context, username, action, resourceType, organizationID, subscriptionID, clusterID string) (allowed bool, err error) {
	return true, nil
}

// ConfigurableMockAuthorization allows configuring per-username permissions for testing
type ConfigurableMockAuthorization struct {
	Permissions map[string][]string // username → []allowedResource
}

var _ Authorization = &ConfigurableMockAuthorization{}

func (m *ConfigurableMockAuthorization) SelfAccessReview(ctx context.Context, action, resourceType, organizationID, subscriptionID, clusterID string) (allowed bool, err error) {
	return true, nil
}

func (m *ConfigurableMockAuthorization) AccessReview(ctx context.Context, username, action, resourceType, organizationID, subscriptionID, clusterID string) (allowed bool, err error) {
	return slices.Contains(m.Permissions[username], resourceType), nil
}

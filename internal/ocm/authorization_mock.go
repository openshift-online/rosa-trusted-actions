package ocm

import (
	"context"
	"slices"
)

// ConfigurableMockAuthorization allows configuring per-username permissions for testing
type ConfigurableMockAuthorization struct {
	Permissions map[string][]string // username → []allowedResource
}

var _ Authorization = &ConfigurableMockAuthorization{}

func (m *ConfigurableMockAuthorization) AccessReview(ctx context.Context, username, action, resourceType string) (allowed bool, err error) {
	return slices.Contains(m.Permissions[username], resourceType), nil
}

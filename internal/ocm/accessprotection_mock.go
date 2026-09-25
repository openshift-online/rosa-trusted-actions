package ocm

import (
	"context"

	acctrspv1 "github.com/openshift-online/ocm-sdk-go/accesstransparency/v1"
)

type ConfigurableMockAccessProtection struct {
	IsEnabled     bool
	AccessRequest *acctrspv1.AccessRequest
}

var _ AccessProtection = &ConfigurableMockAccessProtection{}

func (m *ConfigurableMockAccessProtection) IsAccessProtectionEnabled(ctx context.Context, clusterID string) (enabled bool, err error) {
	return m.IsEnabled, nil
}

func (m *ConfigurableMockAccessProtection) GetClusterActiveAccessRequest(ctx context.Context, clusterID string) (*acctrspv1.AccessRequest, error) {
	return m.AccessRequest, nil
}

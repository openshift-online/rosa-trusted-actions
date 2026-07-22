package backplane

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

var _ ClientProvider = (*KubeconfigProvider)(nil)

type KubeconfigProvider struct {
	logger     *logrus.Logger
	kubeconfig string
}

func NewKubeconfigProvider(logger *logrus.Logger, kubeconfigPath string) *KubeconfigProvider {
	return &KubeconfigProvider{
		logger:     logger,
		kubeconfig: kubeconfigPath,
	}
}

func (k *KubeconfigProvider) GetClient(_ context.Context, clusterID string, rbacRules []RBACRule) (dynamic.Interface, error) {
	k.logger.WithFields(logrus.Fields{
		"cluster_id": clusterID,
		"kubeconfig": k.kubeconfig,
		"rbac_rules": len(rbacRules),
	}).Debug("creating dynamic client from kubeconfig (rbac rules ignored)")

	config, err := clientcmd.BuildConfigFromFlags("", k.kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig %s: %w", k.kubeconfig, err)
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return client, nil
}

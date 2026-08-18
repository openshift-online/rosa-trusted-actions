package backplane

import (
	"bytes"
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
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

func (k *KubeconfigProvider) GetPodExecutor(_ context.Context, clusterID string, rbacRules []RBACRule) (PodExecutor, error) {
	k.logger.WithFields(logrus.Fields{
		"cluster_id": clusterID,
		"kubeconfig": k.kubeconfig,
		"rbac_rules": len(rbacRules),
	}).Debug("creating pod executor from kubeconfig (rbac rules ignored)")

	config, err := clientcmd.BuildConfigFromFlags("", k.kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig %s: %w", k.kubeconfig, err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	return &kubeconfigPodExecutor{config: config, clientset: clientset}, nil
}

type kubeconfigPodExecutor struct {
	config    *rest.Config
	clientset kubernetes.Interface
}

func (e *kubeconfigPodExecutor) Exec(ctx context.Context, namespace, pod, container string, command []string) ([]byte, error) {
	req := e.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(e.config, "POST", req.URL())
	if err != nil {
		return nil, fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return nil, fmt.Errorf("exec failed: %w (stderr: %s)", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

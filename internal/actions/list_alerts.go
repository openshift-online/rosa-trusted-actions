package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
)

const (
	defaultMonitoringNamespace = "openshift-monitoring"
	alertsNamespaceQuery       = `namespace=~"^$|^default$|^openshift-.*|^kube-.*|^redhat-.*",namespace!~"^redhat-rhmi-.*"` // same query as used by managed scripts
	prometheusPort             = 9090
	alertmanagerPort           = 9093
)

type ListAlertsAction struct{}

func NewListAlertsAction() *ListAlertsAction {
	return &ListAlertsAction{}
}

func (l *ListAlertsAction) Name() string      { return "list-alerts" }
func (l *ListAlertsAction) UsesPodExec() bool { return true }

func (l *ListAlertsAction) RequiredRBAC(_ ResourceTarget) []backplane.RBACRule {
	return []backplane.RBACRule{
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list"}},
		{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}},
	}
}

func (l *ListAlertsAction) Execute(ctx context.Context, clients Clients, req ActionRequest) (*ActionResult, error) {
	if clients.PodExecutor == nil {
		return nil, fmt.Errorf("list-alerts requires a pod executor")
	}

	ns := req.Target.Namespace
	if ns == "" {
		ns = defaultMonitoringNamespace
	}

	severity := req.Params["severity"]
	switch severity {
	case "", "critical", "warning":
	default:
		return nil, fmt.Errorf("invalid severity %q, must be one of: critical, warning", severity)
	}
	state := req.Params["state"]
	if state == "" {
		state = "firing"
	}
	switch state {
	case "firing", "pending", "all":
	default:
		return nil, fmt.Errorf("invalid alert state %q, must be one of: firing, pending, all", state)
	}
	includeSilences := req.Params["silences"] == "true"

	promPod, err := discoverPod(ctx, clients.Dynamic, ns, "prometheus")
	if err != nil {
		return nil, fmt.Errorf("failed to discover prometheus pod in %s: %w", ns, err)
	}

	query := buildAlertsQuery(state)
	alertsResp, err := queryViaExec(ctx, clients.PodExecutor, ns, promPod, "prometheus", prometheusPort, "/api/v1/query", map[string]string{
		"query": query,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query prometheus: %w", err)
	}

	criticalAlerts, warningAlerts := extractAlerts(alertsResp, severity)

	result := map[string]interface{}{
		"alerts": map[string]interface{}{
			"critical": criticalAlerts,
			"warning":  warningAlerts,
		},
	}

	var silenceCount int
	if includeSilences {
		amPod, err := discoverPod(ctx, clients.Dynamic, ns, "alertmanager")
		if err != nil {
			return nil, fmt.Errorf("failed to discover alertmanager pod in %s: %w", ns, err)
		}

		silencesResp, err := queryViaExecArray(ctx, clients.PodExecutor, ns, amPod, "alertmanager", alertmanagerPort, "/api/v2/silences", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to query alertmanager silences: %w", err)
		}

		silences := extractSilences(silencesResp)
		silenceCount = len(silences)
		result["silences"] = silences
	}

	msg := fmt.Sprintf("%d critical, %d warning alerts", len(criticalAlerts), len(warningAlerts))
	if includeSilences {
		msg += fmt.Sprintf(", %d active silences", silenceCount)
	}

	return &ActionResult{
		Resources: []unstructured.Unstructured{
			{Object: result},
		},
		Message: msg,
	}, nil
}

// discoverPod finds a pod in the given namespace by matching on the
// app.kubernetes.io/name label. This avoids hardcoding pod names like
// "prometheus-k8s-0" or "alertmanager-main-0" which could change.
func discoverPod(ctx context.Context, client dynamic.Interface, namespace, appName string) (string, error) {
	pods, err := client.Resource(podsGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=" + appName,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no %s pod found in namespace %s", appName, namespace)
	}

	return pods.Items[0].GetName(), nil
}

func queryViaExecArray(ctx context.Context, executor backplane.PodExecutor, namespace, pod, container string, port int, path string, params map[string]string) ([]interface{}, error) {
	raw, err := execCurl(ctx, executor, namespace, pod, container, port, path, params)
	if err != nil {
		return nil, err
	}

	var result []interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}

func queryViaExec(ctx context.Context, executor backplane.PodExecutor, namespace, pod, container string, port int, path string, params map[string]string) (map[string]interface{}, error) {
	raw, err := execCurl(ctx, executor, namespace, pod, container, port, path, params)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}

func execCurl(ctx context.Context, executor backplane.PodExecutor, namespace, pod, container string, port int, path string, params map[string]string) ([]byte, error) {
	u := fmt.Sprintf("http://localhost:%d%s", port, path)
	if len(params) > 0 {
		vals := url.Values{}
		for k, v := range params {
			vals.Set(k, v)
		}
		u += "?" + vals.Encode()
	}

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	raw, err := executor.Exec(execCtx, namespace, pod, container, []string{"curl", "-sfS", "--max-time", "25", u})
	if err != nil {
		return nil, fmt.Errorf("exec failed: %w", err)
	}

	return raw, nil
}

func buildAlertsQuery(alertstate string) string {
	var parts []string

	switch alertstate {
	case "firing":
		parts = append(parts, `alertstate="firing"`)
	case "pending":
		parts = append(parts, `alertstate="pending"`)
	case "all":
		// no alertstate filter — include firing + pending
	}

	parts = append(parts, alertsNamespaceQuery)

	return "ALERTS{" + strings.Join(parts, ",") + "}"
}

func extractAlerts(resp map[string]interface{}, severity string) ([]interface{}, []interface{}) {
	var critical, warning []interface{}

	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		return critical, warning
	}

	results, ok := data["result"].([]interface{})
	if !ok {
		return critical, warning
	}

	includeCritical := severity == "" || severity == "critical"
	includeWarning := severity == "" || severity == "warning"

	for _, r := range results {
		entry, ok := r.(map[string]interface{})
		if !ok {
			continue
		}

		metric, ok := entry["metric"].(map[string]interface{})
		if !ok {
			continue
		}

		sev, _ := metric["severity"].(string)
		alert := extractAlert(metric)

		switch {
		case sev == "critical" && includeCritical:
			critical = append(critical, alert)
		case sev == "warning" && includeWarning:
			warning = append(warning, alert)
		}
	}

	if critical == nil {
		critical = []interface{}{}
	}
	if warning == nil {
		warning = []interface{}{}
	}

	return critical, warning
}

func extractAlert(metric map[string]interface{}) map[string]interface{} {
	alert := make(map[string]interface{})
	for _, key := range []string{"alertname", "job", "namespace", "exported_namespace", "pod", "service", "alertstate"} {
		setIfPresent(alert, key, metric[key])
	}
	return alert
}

func extractSilences(data []interface{}) []interface{} {
	var active []interface{}
	for _, s := range data {
		silence, ok := s.(map[string]interface{})
		if !ok {
			continue
		}

		status, ok := silence["status"].(map[string]interface{})
		if !ok {
			continue
		}

		state, _ := status["state"].(string)
		if state != "active" {
			continue
		}

		entry := make(map[string]interface{})
		for _, key := range []string{"id", "createdBy", "comment", "startsAt", "endsAt", "matchers"} {
			setIfPresent(entry, key, silence[key])
		}
		active = append(active, entry)
	}

	if active == nil {
		active = []interface{}{}
	}
	return active
}

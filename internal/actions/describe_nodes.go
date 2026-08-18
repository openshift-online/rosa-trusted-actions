package actions

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
)

var _ Action = (*DescribeNodesAction)(nil)

var (
	nodesGVR = schema.GroupVersionResource{
		Group: "", Version: "v1", Resource: "nodes",
	}
	podsGVR = schema.GroupVersionResource{
		Group: "", Version: "v1", Resource: "pods",
	}
	eventsGVR = schema.GroupVersionResource{
		Group: "", Version: "v1", Resource: "events",
	}
	leasesGVR = schema.GroupVersionResource{
		Group: "coordination.k8s.io", Version: "v1", Resource: "leases",
	}
)

const kubeNodeLeaseNamespace = "kube-node-lease"

type DescribeNodesAction struct{}

func NewDescribeNodesAction() *DescribeNodesAction {
	return &DescribeNodesAction{}
}

func (d *DescribeNodesAction) Name() string      { return "describe-nodes" }
func (d *DescribeNodesAction) UsesPodExec() bool { return false }

func (d *DescribeNodesAction) RequiredRBAC(target ResourceTarget) []backplane.RBACRule {
	nodesRule := backplane.RBACRule{
		APIGroups: []string{""},
		Resources: []string{"nodes"},
	}
	if target.Name != "" {
		nodesRule.Verbs = []string{"get"}
		nodesRule.ResourceNames = []string{target.Name}
	} else {
		nodesRule.Verbs = []string{"list"}
	}

	return []backplane.RBACRule{
		nodesRule,
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list"}},
		{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"list"}},
		{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"list"}},
	}
}

func (d *DescribeNodesAction) Execute(ctx context.Context, clients Clients, req ActionRequest) (*ActionResult, error) {
	var nodeItems []unstructured.Unstructured
	if req.Target.Name != "" {
		node, err := clients.Dynamic.Resource(nodesGVR).Get(ctx, req.Target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get node %s: %w", req.Target.Name, err)
		}
		nodeItems = []unstructured.Unstructured{*node}
	} else {
		nodes, err := clients.Dynamic.Resource(nodesGVR).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list nodes: %w", err)
		}
		nodeItems = nodes.Items
	}

	podSelector := "status.phase!=Succeeded,status.phase!=Failed"
	eventSelector := "involvedObject.kind=Node"
	if req.Target.Name != "" {
		podSelector += ",spec.nodeName=" + req.Target.Name
		eventSelector += ",involvedObject.name=" + req.Target.Name
	}

	pods, err := clients.Dynamic.Resource(podsGVR).Namespace("").List(ctx, metav1.ListOptions{
		FieldSelector: podSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	events, err := clients.Dynamic.Resource(eventsGVR).Namespace("").List(ctx, metav1.ListOptions{
		FieldSelector: eventSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	leases, err := clients.Dynamic.Resource(leasesGVR).Namespace(kubeNodeLeaseNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list leases: %w", err)
	}
	leaseByNode := indexLeasesByNode(leases.Items)

	podsByNode := groupPodsByNode(pods.Items)
	eventsByNode := groupEventsByNode(events.Items)

	resources := make([]unstructured.Unstructured, 0, len(nodeItems))
	for _, node := range nodeItems {
		name := node.GetName()
		resources = append(resources, reshapeNode(node, podsByNode[name], eventsByNode[name], leaseByNode[name]))
	}

	msg := fmt.Sprintf("described %d nodes", len(resources))
	if req.Target.Name != "" {
		msg = fmt.Sprintf("described node %s", req.Target.Name)
	}

	return &ActionResult{
		Resources: resources,
		Message:   msg,
	}, nil
}

func groupPodsByNode(pods []unstructured.Unstructured) map[string][]interface{} {
	result := make(map[string][]interface{})
	for _, pod := range pods {
		spec, ok := pod.Object["spec"].(map[string]interface{})
		if !ok {
			continue
		}
		nodeName, ok := spec["nodeName"].(string)
		if !ok || nodeName == "" {
			continue
		}
		result[nodeName] = append(result[nodeName], extractPod(pod))
	}
	return result
}

func groupEventsByNode(events []unstructured.Unstructured) map[string][]interface{} {
	result := make(map[string][]interface{})
	for _, event := range events {
		involved, ok := event.Object["involvedObject"].(map[string]interface{})
		if !ok {
			continue
		}
		kind, _ := involved["kind"].(string)
		if kind != "Node" {
			continue
		}
		name, ok := involved["name"].(string)
		if !ok || name == "" {
			continue
		}
		result[name] = append(result[name], extractEvent(event))
	}
	return result
}

func indexLeasesByNode(leases []unstructured.Unstructured) map[string]interface{} {
	result := make(map[string]interface{})
	for _, lease := range leases {
		name := lease.GetName()
		if name == "" {
			continue
		}
		result[name] = lease.Object["spec"]
	}
	return result
}

// reshapeNode correlates the four resources that back `oc describe node` into a single flat object.
func reshapeNode(node unstructured.Unstructured, pods []interface{}, events []interface{}, lease interface{}) unstructured.Unstructured {
	metadata, _ := node.Object["metadata"].(map[string]interface{})
	spec, _ := node.Object["spec"].(map[string]interface{})
	status, _ := node.Object["status"].(map[string]interface{})

	if pods == nil {
		pods = []interface{}{}
	}
	if events == nil {
		events = []interface{}{}
	}

	out := map[string]interface{}{
		"name":   node.GetName(),
		"pods":   pods,
		"events": events,
		"lease":  lease,
	}

	if metadata != nil {
		setIfPresent(out, "labels", metadata["labels"])
		setIfPresent(out, "annotations", metadata["annotations"])
		setIfPresent(out, "creationTimestamp", metadata["creationTimestamp"])
	}

	if spec != nil {
		setIfPresent(out, "taints", spec["taints"])
		setIfPresent(out, "unschedulable", spec["unschedulable"])
		setIfPresent(out, "providerID", spec["providerID"])
		setIfPresent(out, "podCIDR", spec["podCIDR"])
		setIfPresent(out, "podCIDRs", spec["podCIDRs"])
	}

	if status != nil {
		setIfPresent(out, "conditions", status["conditions"])
		setIfPresent(out, "addresses", status["addresses"])
		setIfPresent(out, "capacity", status["capacity"])
		setIfPresent(out, "allocatable", status["allocatable"])
		setIfPresent(out, "systemInfo", status["nodeInfo"])
	}

	return unstructured.Unstructured{Object: out}
}


func extractPod(pod unstructured.Unstructured) map[string]interface{} {
	spec, _ := pod.Object["spec"].(map[string]interface{})
	status, _ := pod.Object["status"].(map[string]interface{})

	result := map[string]interface{}{
		"name":      pod.GetName(),
		"namespace": pod.GetNamespace(),
	}

	if spec != nil {
		result["containers"] = extractContainers(spec["containers"])
		result["initContainers"] = extractContainers(spec["initContainers"])
	}

	if status != nil {
		setIfPresent(result, "phase", status["phase"])
		setIfPresent(result, "startTime", status["startTime"])
		result["containerStatuses"] = extractContainerStatuses(status["containerStatuses"])
		result["initContainerStatuses"] = extractContainerStatuses(status["initContainerStatuses"])
	}

	return result
}

func extractContainers(containersRaw interface{}) []interface{} {
	containers, ok := containersRaw.([]interface{})
	if !ok {
		return []interface{}{}
	}

	result := make([]interface{}, 0, len(containers))
	for _, c := range containers {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		entry := map[string]interface{}{
			"name": cm["name"],
		}
		if resources, ok := cm["resources"]; ok {
			entry["resources"] = resources
		}
		result = append(result, entry)
	}
	return result
}

func extractContainerStatuses(raw interface{}) []interface{} {
	statuses, ok := raw.([]interface{})
	if !ok {
		return []interface{}{}
	}

	result := make([]interface{}, 0, len(statuses))
	for _, s := range statuses {
		sm, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		entry := map[string]interface{}{
			"name": sm["name"],
		}
		setIfPresent(entry, "ready", sm["ready"])
		setIfPresent(entry, "restartCount", sm["restartCount"])
		setIfPresent(entry, "state", sm["state"])
		setIfPresent(entry, "lastState", sm["lastState"])
		result = append(result, entry)
	}
	return result
}

func extractEvent(event unstructured.Unstructured) map[string]interface{} {
	result := make(map[string]interface{})
	for _, key := range []string{"type", "reason", "message", "source", "firstTimestamp", "lastTimestamp", "count"} {
		setIfPresent(result, key, event.Object[key])
	}
	return result
}

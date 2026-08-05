package actions

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func newDescribeNodesFakeClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	gvrToListKind := map[schema.GroupVersionResource]string{
		nodesGVR:  "NodeList",
		podsGVR:   "PodList",
		eventsGVR: "EventList",
		leasesGVR: "LeaseList",
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind, objects...)
}

func newFullNode(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Node",
			"metadata": map[string]interface{}{
				"name":              name,
				"labels":            map[string]interface{}{"kubernetes.io/os": "linux"},
				"annotations":       map[string]interface{}{"node.alpha.kubernetes.io/ttl": "0"},
				"creationTimestamp": "2024-01-15T10:30:00Z",
			},
			"spec": map[string]interface{}{
				"providerID": "aws:///us-east-1a/i-" + name,
				"podCIDR":    "10.128.0.0/24",
				"podCIDRs":   []interface{}{"10.128.0.0/24"},
			},
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"type":   "Ready",
						"status": "True",
					},
				},
				"addresses": []interface{}{
					map[string]interface{}{
						"type":    "InternalIP",
						"address": "10.0.1.1",
					},
				},
				"capacity": map[string]interface{}{
					"cpu":    "4",
					"memory": "16Gi",
				},
				"allocatable": map[string]interface{}{
					"cpu":    "3800m",
					"memory": "15Gi",
				},
				"nodeInfo": map[string]interface{}{
					"kubeletVersion":  "v1.28.0",
					"operatingSystem": "linux",
					"architecture":    "amd64",
				},
			},
		},
	}
}

func newPod(namespace, name, nodeName string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
			"spec": map[string]interface{}{
				"nodeName": nodeName,
				"containers": []interface{}{
					map[string]interface{}{
						"name": "main",
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{
								"cpu":    "100m",
								"memory": "128Mi",
							},
							"limits": map[string]interface{}{
								"cpu":    "200m",
								"memory": "256Mi",
							},
						},
					},
				},
			},
			"status": map[string]interface{}{
				"phase":     "Running",
				"startTime": "2024-01-15T10:30:00Z",
				"containerStatuses": []interface{}{
					map[string]interface{}{
						"name":         "main",
						"ready":        true,
						"restartCount": int64(0),
						"state": map[string]interface{}{
							"running": map[string]interface{}{
								"startedAt": "2024-01-15T10:30:05Z",
							},
						},
					},
				},
			},
		},
	}
}

func newNodeEvent(nodeName, reason, message string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Event",
			"metadata": map[string]interface{}{
				"name":      nodeName + "-" + reason,
				"namespace": "default",
			},
			"involvedObject": map[string]interface{}{
				"kind": "Node",
				"name": nodeName,
			},
			"type":           "Normal",
			"reason":         reason,
			"message":        message,
			"firstTimestamp": "2024-01-15T10:30:00Z",
			"lastTimestamp":  "2024-01-15T10:35:00Z",
			"count":          int64(1),
		},
	}
}

func newLease(nodeName string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "coordination.k8s.io/v1",
			"kind":       "Lease",
			"metadata": map[string]interface{}{
				"name":      nodeName,
				"namespace": "kube-node-lease",
			},
			"spec": map[string]interface{}{
				"holderIdentity":       nodeName,
				"leaseDurationSeconds": int64(40),
				"renewTime":            "2024-01-15T10:35:00Z",
			},
		},
	}
}

func TestDescribeNodesAction_Name(t *testing.T) {
	action := NewDescribeNodesAction()
	if action.Name() != "describe-nodes" {
		t.Errorf("expected name %q, got %q", "describe-nodes", action.Name())
	}
}

func TestDescribeNodesAction_RequiredRBAC(t *testing.T) {
	action := NewDescribeNodesAction()
	rules := action.RequiredRBAC(ResourceTarget{})

	if len(rules) != 4 {
		t.Fatalf("expected 4 RBAC rules, got %d", len(rules))
	}

	expected := []struct {
		apiGroup string
		resource string
		verb     string
	}{
		{"", "nodes", "list"},
		{"", "pods", "list"},
		{"", "events", "list"},
		{"coordination.k8s.io", "leases", "list"},
	}

	for i, exp := range expected {
		if rules[i].APIGroups[0] != exp.apiGroup {
			t.Errorf("rule %d: expected API group %q, got %q", i, exp.apiGroup, rules[i].APIGroups[0])
		}
		if rules[i].Resources[0] != exp.resource {
			t.Errorf("rule %d: expected resource %q, got %q", i, exp.resource, rules[i].Resources[0])
		}
		if rules[i].Verbs[0] != exp.verb {
			t.Errorf("rule %d: expected verb %q, got %q", i, exp.verb, rules[i].Verbs[0])
		}
	}
}

func TestDescribeNodesAction_Execute_EmptyCluster(t *testing.T) {
	client := newDescribeNodesFakeClient()
	action := NewDescribeNodesAction()

	result, err := action.Execute(context.Background(), client, ActionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(result.Resources))
	}
	if result.Message != "described 0 nodes" {
		t.Errorf("expected message %q, got %q", "described 0 nodes", result.Message)
	}
}

func TestDescribeNodesAction_Execute_SingleNode(t *testing.T) {
	client := newDescribeNodesFakeClient(newFullNode("node-a"))
	action := NewDescribeNodesAction()

	result, err := action.Execute(context.Background(), client, ActionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}

	node := result.Resources[0].Object
	if node["name"] != "node-a" {
		t.Errorf("expected name %q, got %v", "node-a", node["name"])
	}

	pods, ok := node["pods"].([]interface{})
	if !ok {
		t.Fatal("expected pods to be a slice")
	}
	if len(pods) != 0 {
		t.Errorf("expected 0 pods, got %d", len(pods))
	}

	events, ok := node["events"].([]interface{})
	if !ok {
		t.Fatal("expected events to be a slice")
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}

	if node["lease"] != nil {
		t.Errorf("expected nil lease, got %v", node["lease"])
	}

	if node["conditions"] == nil {
		t.Error("expected conditions to be present")
	}
	if node["capacity"] == nil {
		t.Error("expected capacity to be present")
	}
	if node["allocatable"] == nil {
		t.Error("expected allocatable to be present")
	}
	if node["systemInfo"] == nil {
		t.Error("expected systemInfo to be present")
	}
}

func TestDescribeNodesAction_Execute_MultipleNodes(t *testing.T) {
	client := newDescribeNodesFakeClient(
		newFullNode("node-a"),
		newFullNode("node-b"),
		newFullNode("node-c"),
	)
	action := NewDescribeNodesAction()

	result, err := action.Execute(context.Background(), client, ActionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Resources) != 3 {
		t.Errorf("expected 3 resources, got %d", len(result.Resources))
	}
	if result.Message != "described 3 nodes" {
		t.Errorf("expected message %q, got %q", "described 3 nodes", result.Message)
	}
}

func TestDescribeNodesAction_Execute_PodCorrelation(t *testing.T) {
	client := newDescribeNodesFakeClient(
		newFullNode("node-a"),
		newFullNode("node-b"),
		newPod("ns-1", "pod-1", "node-a"),
		newPod("ns-1", "pod-2", "node-a"),
		newPod("ns-2", "pod-3", "node-b"),
	)
	action := NewDescribeNodesAction()

	result, err := action.Execute(context.Background(), client, ActionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := make(map[string]map[string]interface{})
	for _, r := range result.Resources {
		name, _ := r.Object["name"].(string)
		nodeMap[name] = r.Object
	}

	podsA, ok := nodeMap["node-a"]["pods"].([]interface{})
	if !ok {
		t.Fatal("expected node-a pods to be a slice")
	}
	if len(podsA) != 2 {
		t.Errorf("expected 2 pods on node-a, got %d", len(podsA))
	}

	podsB, ok := nodeMap["node-b"]["pods"].([]interface{})
	if !ok {
		t.Fatal("expected node-b pods to be a slice")
	}
	if len(podsB) != 1 {
		t.Errorf("expected 1 pod on node-b, got %d", len(podsB))
	}
}

func TestDescribeNodesAction_Execute_PodContainerResources(t *testing.T) {
	pod := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"namespace": "test-ns",
				"name":      "multi-container-pod",
			},
			"spec": map[string]interface{}{
				"nodeName": "node-a",
				"containers": []interface{}{
					map[string]interface{}{
						"name": "app",
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{"cpu": "100m"},
						},
					},
					map[string]interface{}{
						"name": "sidecar",
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{"cpu": "50m"},
						},
					},
				},
				"initContainers": []interface{}{
					map[string]interface{}{
						"name": "init",
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{"cpu": "200m"},
						},
					},
				},
			},
		},
	}
	client := newDescribeNodesFakeClient(newFullNode("node-a"), pod)
	action := NewDescribeNodesAction()

	result, err := action.Execute(context.Background(), client, ActionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pods, ok := result.Resources[0].Object["pods"].([]interface{})
	if !ok {
		t.Fatal("expected pods to be a slice")
	}
	if len(pods) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(pods))
	}

	p, ok := pods[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected pod to be a map")
	}
	containers, ok := p["containers"].([]interface{})
	if !ok {
		t.Fatal("expected containers to be a slice")
	}
	if len(containers) != 2 {
		t.Errorf("expected 2 containers, got %d", len(containers))
	}

	initContainers, ok := p["initContainers"].([]interface{})
	if !ok {
		t.Fatal("expected initContainers to be a slice")
	}
	if len(initContainers) != 1 {
		t.Errorf("expected 1 init container, got %d", len(initContainers))
	}

	container, ok := containers[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected container to be a map")
	}
	if container["name"] != "app" {
		t.Errorf("expected container name %q, got %v", "app", container["name"])
	}
	resources, ok := container["resources"].(map[string]interface{})
	if !ok {
		t.Fatal("expected container resources to be a map")
	}
	requests, ok := resources["requests"].(map[string]interface{})
	if !ok {
		t.Fatal("expected requests to be a map")
	}
	if requests["cpu"] != "100m" {
		t.Errorf("expected cpu request %q, got %v", "100m", requests["cpu"])
	}
}

func TestDescribeNodesAction_Execute_PodStatus(t *testing.T) {
	pod := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"namespace": "test-ns",
				"name":      "crashing-pod",
			},
			"spec": map[string]interface{}{
				"nodeName": "node-a",
				"containers": []interface{}{
					map[string]interface{}{"name": "app"},
				},
			},
			"status": map[string]interface{}{
				"phase":     "Running",
				"startTime": "2024-01-15T10:30:00Z",
				"containerStatuses": []interface{}{
					map[string]interface{}{
						"name":         "app",
						"ready":        false,
						"restartCount": int64(5),
						"state": map[string]interface{}{
							"waiting": map[string]interface{}{
								"reason":  "CrashLoopBackOff",
								"message": "back-off 5m0s restarting failed container",
							},
						},
						"lastState": map[string]interface{}{
							"terminated": map[string]interface{}{
								"exitCode": int64(1),
								"reason":   "Error",
							},
						},
					},
				},
			},
		},
	}
	client := newDescribeNodesFakeClient(newFullNode("node-a"), pod)
	action := NewDescribeNodesAction()

	result, err := action.Execute(context.Background(), client, ActionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pods, ok := result.Resources[0].Object["pods"].([]interface{})
	if !ok {
		t.Fatal("expected pods to be a slice")
	}
	if len(pods) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(pods))
	}

	p, ok := pods[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected pod to be a map")
	}
	if p["phase"] != "Running" {
		t.Errorf("expected phase %q, got %v", "Running", p["phase"])
	}
	if p["startTime"] != "2024-01-15T10:30:00Z" {
		t.Errorf("expected startTime %q, got %v", "2024-01-15T10:30:00Z", p["startTime"])
	}

	statuses, ok := p["containerStatuses"].([]interface{})
	if !ok {
		t.Fatal("expected containerStatuses to be a slice")
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 container status, got %d", len(statuses))
	}

	cs, ok := statuses[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected container status to be a map")
	}
	if cs["name"] != "app" {
		t.Errorf("expected container status name %q, got %v", "app", cs["name"])
	}
	if cs["ready"] != false {
		t.Errorf("expected ready to be false, got %v", cs["ready"])
	}
	if cs["restartCount"] != int64(5) {
		t.Errorf("expected restartCount 5, got %v", cs["restartCount"])
	}
	if cs["state"] == nil {
		t.Error("expected state to be present")
	}
	if cs["lastState"] == nil {
		t.Error("expected lastState to be present")
	}
}

func TestDescribeNodesAction_Execute_EventCorrelation(t *testing.T) {
	client := newDescribeNodesFakeClient(
		newFullNode("node-a"),
		newFullNode("node-b"),
		newNodeEvent("node-a", "NodeReady", "Node node-a is ready"),
		newNodeEvent("node-a", "NodeAllocatable", "Updated limits"),
		newNodeEvent("node-b", "NodeReady", "Node node-b is ready"),
	)
	action := NewDescribeNodesAction()

	result, err := action.Execute(context.Background(), client, ActionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := make(map[string]map[string]interface{})
	for _, r := range result.Resources {
		name, _ := r.Object["name"].(string)
		nodeMap[name] = r.Object
	}

	eventsA, ok := nodeMap["node-a"]["events"].([]interface{})
	if !ok {
		t.Fatal("expected node-a events to be a slice")
	}
	if len(eventsA) != 2 {
		t.Errorf("expected 2 events on node-a, got %d", len(eventsA))
	}

	eventsB, ok := nodeMap["node-b"]["events"].([]interface{})
	if !ok {
		t.Fatal("expected node-b events to be a slice")
	}
	if len(eventsB) != 1 {
		t.Errorf("expected 1 event on node-b, got %d", len(eventsB))
	}

	event, ok := eventsA[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected event to be a map")
	}
	if event["reason"] == nil {
		t.Error("expected event reason to be present")
	}
	if event["message"] == nil {
		t.Error("expected event message to be present")
	}
}

func TestDescribeNodesAction_Execute_LeaseCorrelation(t *testing.T) {
	client := newDescribeNodesFakeClient(
		newFullNode("node-a"),
		newFullNode("node-b"),
		newLease("node-a"),
	)
	action := NewDescribeNodesAction()

	result, err := action.Execute(context.Background(), client, ActionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := make(map[string]map[string]interface{})
	for _, r := range result.Resources {
		name, _ := r.Object["name"].(string)
		nodeMap[name] = r.Object
	}

	leaseA := nodeMap["node-a"]["lease"]
	if leaseA == nil {
		t.Fatal("expected node-a to have a lease")
	}
	leaseSpec, ok := leaseA.(map[string]interface{})
	if !ok {
		t.Fatal("expected lease to be a map")
	}
	if leaseSpec["holderIdentity"] != "node-a" {
		t.Errorf("expected holder identity %q, got %v", "node-a", leaseSpec["holderIdentity"])
	}

	leaseB := nodeMap["node-b"]["lease"]
	if leaseB != nil {
		t.Errorf("expected node-b lease to be nil, got %v", leaseB)
	}
}

func TestDescribeNodesAction_Execute_FullCorrelation(t *testing.T) {
	client := newDescribeNodesFakeClient(
		newFullNode("node-a"),
		newFullNode("node-b"),
		newPod("ns-1", "pod-1", "node-a"),
		newPod("ns-2", "pod-2", "node-b"),
		newNodeEvent("node-a", "NodeReady", "ready"),
		newLease("node-a"),
		newLease("node-b"),
	)
	action := NewDescribeNodesAction()

	result, err := action.Execute(context.Background(), client, ActionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(result.Resources))
	}

	nodeMap := make(map[string]map[string]interface{})
	for _, r := range result.Resources {
		name, _ := r.Object["name"].(string)
		nodeMap[name] = r.Object
	}

	podsA, ok := nodeMap["node-a"]["pods"].([]interface{})
	if !ok {
		t.Fatal("expected node-a pods to be a slice")
	}
	if len(podsA) != 1 {
		t.Errorf("expected 1 pod on node-a, got %d", len(podsA))
	}
	eventsA, ok := nodeMap["node-a"]["events"].([]interface{})
	if !ok {
		t.Fatal("expected node-a events to be a slice")
	}
	if len(eventsA) != 1 {
		t.Errorf("expected 1 event on node-a, got %d", len(eventsA))
	}
	if nodeMap["node-a"]["lease"] == nil {
		t.Error("expected node-a to have a lease")
	}

	podsB, ok := nodeMap["node-b"]["pods"].([]interface{})
	if !ok {
		t.Fatal("expected node-b pods to be a slice")
	}
	if len(podsB) != 1 {
		t.Errorf("expected 1 pod on node-b, got %d", len(podsB))
	}
	eventsB, ok := nodeMap["node-b"]["events"].([]interface{})
	if !ok {
		t.Fatal("expected node-b events to be a slice")
	}
	if len(eventsB) != 0 {
		t.Errorf("expected 0 events on node-b, got %d", len(eventsB))
	}
	if nodeMap["node-b"]["lease"] == nil {
		t.Error("expected node-b to have a lease")
	}
}

func TestDescribeNodesAction_Execute_NodeTaintsAndUnschedulable(t *testing.T) {
	node := newFullNode("tainted-node")
	spec, ok := node.Object["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("expected spec to be a map")
	}
	spec["taints"] = []interface{}{
		map[string]interface{}{
			"key":    "node-role.kubernetes.io/infra",
			"effect": "NoSchedule",
		},
	}
	spec["unschedulable"] = true

	client := newDescribeNodesFakeClient(node)
	action := NewDescribeNodesAction()

	result, err := action.Execute(context.Background(), client, ActionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := result.Resources[0].Object
	taints, ok := out["taints"].([]interface{})
	if !ok {
		t.Fatal("expected taints to be a slice")
	}
	if len(taints) != 1 {
		t.Errorf("expected 1 taint, got %d", len(taints))
	}

	unschedulable, ok := out["unschedulable"].(bool)
	if !ok {
		t.Fatal("expected unschedulable to be a bool")
	}
	if !unschedulable {
		t.Error("expected unschedulable to be true")
	}
}

func TestDescribeNodesAction_RequiredRBAC_SingleNode(t *testing.T) {
	action := NewDescribeNodesAction()
	rules := action.RequiredRBAC(ResourceTarget{Name: "node-a"})

	if len(rules) != 4 {
		t.Fatalf("expected 4 RBAC rules, got %d", len(rules))
	}

	nodesRule := rules[0]
	if nodesRule.Verbs[0] != "get" {
		t.Errorf("expected nodes verb %q, got %q", "get", nodesRule.Verbs[0])
	}
	if len(nodesRule.ResourceNames) != 1 || nodesRule.ResourceNames[0] != "node-a" {
		t.Errorf("expected ResourceNames [node-a], got %v", nodesRule.ResourceNames)
	}

	for i := 1; i < 4; i++ {
		if rules[i].Verbs[0] != "list" {
			t.Errorf("rule %d: expected verb %q, got %q", i, "list", rules[i].Verbs[0])
		}
	}
}

func TestDescribeNodesAction_Execute_SingleNodeByName(t *testing.T) {
	client := newDescribeNodesFakeClient(
		newFullNode("node-a"),
		newFullNode("node-b"),
		newPod("ns-1", "pod-1", "node-a"),
		newPod("ns-1", "pod-2", "node-b"),
		newNodeEvent("node-a", "NodeReady", "ready"),
		newLease("node-a"),
		newLease("node-b"),
	)
	action := NewDescribeNodesAction()

	result, err := action.Execute(context.Background(), client, ActionRequest{
		Target: ResourceTarget{Name: "node-a"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}
	if result.Message != "described node node-a" {
		t.Errorf("expected message %q, got %q", "described node node-a", result.Message)
	}

	node := result.Resources[0].Object
	if node["name"] != "node-a" {
		t.Errorf("expected name %q, got %v", "node-a", node["name"])
	}

	pods, ok := node["pods"].([]interface{})
	if !ok {
		t.Fatal("expected pods to be a slice")
	}
	if len(pods) != 1 {
		t.Errorf("expected 1 pod, got %d", len(pods))
	}

	events, ok := node["events"].([]interface{})
	if !ok {
		t.Fatal("expected events to be a slice")
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}

	if node["lease"] == nil {
		t.Error("expected lease to be present")
	}
}

func TestDescribeNodesAction_Execute_SingleNodeNotFound(t *testing.T) {
	client := newDescribeNodesFakeClient(newFullNode("node-a"))
	action := NewDescribeNodesAction()

	_, err := action.Execute(context.Background(), client, ActionRequest{
		Target: ResourceTarget{Name: "nonexistent"},
	})
	if err == nil {
		t.Fatal("expected error for nonexistent node, got nil")
	}
}

func TestDescribeNodesAction_Execute_SingleNodeFieldSelectors(t *testing.T) {
	client := newDescribeNodesFakeClient(
		newFullNode("node-a"),
		newPod("ns-1", "pod-1", "node-a"),
		newNodeEvent("node-a", "NodeReady", "ready"),
		newLease("node-a"),
	)
	action := NewDescribeNodesAction()

	_, err := action.Execute(context.Background(), client, ActionRequest{
		Target: ResourceTarget{Name: "node-a"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, a := range client.Actions() {
		if a.GetVerb() != "list" {
			continue
		}
		la, ok := a.(listAction)
		if !ok {
			continue
		}
		restrictions := la.GetListRestrictions()
		selector := restrictions.Fields.String()
		switch a.GetResource().Resource {
		case "pods":
			if expected := "spec.nodeName=node-a"; !containsSelector(selector, expected) {
				t.Errorf("expected pod field selector to contain %q, got %q", expected, selector)
			}
		case "events":
			if expected := "involvedObject.name=node-a"; !containsSelector(selector, expected) {
				t.Errorf("expected event field selector to contain %q, got %q", expected, selector)
			}
		}
	}
}

type listAction interface {
	GetListRestrictions() ktesting.ListRestrictions
}

func containsSelector(selector, expected string) bool {
	for _, part := range strings.Split(selector, ",") {
		if strings.TrimSpace(part) == expected {
			return true
		}
	}
	return false
}

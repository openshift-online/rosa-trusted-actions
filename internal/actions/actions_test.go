package actions

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/restmapper"

	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
)

func newFakeClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	return dynamicfake.NewSimpleDynamicClient(scheme, objects...)
}

func newClientOject(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
		},
	}
}

func newDataObject(kind, namespace, name string, data map[string]interface{}) *unstructured.Unstructured {
	obj := newClientOject("v1", kind, namespace, name)
	obj.Object["data"] = data
	return obj
}

func defaultData(key, value string) map[string]interface{} {
	return map[string]interface{}{
		key: value,
	}
}

func newConfigMap(namespace, name, key, value string) *unstructured.Unstructured {
	return newDataObject("ConfigMap", namespace, name, defaultData(key, value))
}

func newSecret(namespace, name, key, value string) *unstructured.Unstructured {
	return newDataObject("Secret", namespace, name, defaultData(key, value))
}

func newNode(name string, labels map[string]interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Node",
			"metadata": map[string]interface{}{
				"name":   name,
				"labels": labels,
			},
		},
	}
}

var noLabels = map[string]interface{}{}

// Config maps
var (
	myConfigMap       = newConfigMap("openshift-monitoring", "my-config", "my-config-key", "my-config-value")
	cm1               = newConfigMap("openshift-monitoring", "config-1", "config-1-key", "config-1-value")
	cm2               = newConfigMap("openshift-monitoring", "config-2", "config-2-key", "config-2-value")
	myConfigMapTarget = ResourceTarget{
		Group:     "",
		Version:   "v1",
		Resource:  "configmaps",
		Namespace: "openshift-monitoring",
		Name:      "my-config",
	}
)

// Secrets
var (
	mySecret       = newSecret("openshift-monitoring", "my-secret", "my-secret-key", "my-secret-value")
	mySecretTarget = ResourceTarget{
		Group:     "",
		Version:   "v1",
		Resource:  "secrets",
		Namespace: "openshift-monitoring",
		Name:      "my-secret",
	}
)

// Nodes
var (
	myNode       = newNode("my-node", noLabels)
	n1           = newNode("node-1", noLabels)
	n2           = newNode("node-2", noLabels)
	myNodeTarget = ResourceTarget{
		Group:         "",
		Version:       "v1",
		Resource:      "nodes",
		ClusterScoped: true,
		Name:          "my-node",
	}
)

var mapper = restmapper.NewDiscoveryRESTMapper([]*restmapper.APIGroupResources{
	{
		Group: metav1.APIGroup{
			Name: "",
			Versions: []metav1.GroupVersionForDiscovery{
				{
					GroupVersion: "v1",
					Version:      "v1",
				},
			},
		},
		VersionedResources: map[string][]metav1.APIResource{
			"v1": {
				{
					Name:       "configmaps",
					Namespaced: true,
					Kind:       "ConfigMap",
				},
				{
					Name:       "secrets",
					Namespaced: true,
					Kind:       "Secret",
				},
				{
					Name:       "nodes",
					Namespaced: false,
					Kind:       "Node",
				},
			},
		},
	},
})

func TestAction_Name(t *testing.T) {
	tests := []struct {
		actionImplName string
		action         Action
		actionName     string
	}{
		{
			actionImplName: "GetAction",
			action:         NewGetAction(),
			actionName:     "get",
		},
		{
			actionImplName: "PatchAction",
			action:         NewPatchAction(),
			actionName:     "patch",
		},
		{
			actionImplName: "DeleteAction",
			action:         NewDeleteAction(),
			actionName:     "delete",
		},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("When using a %s action, name should be %s", test.action.Name(), test.actionName), func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(test.action.Name()).To(Equal(test.actionName))
		})
	}
}

func TestAction_RequiredRBAC(t *testing.T) {
	tests := []struct {
		action            Action
		testName          string
		target            ResourceTarget
		expectedRBACRules []backplane.RBACRule
	}{
		// GET action tests
		{
			action:   NewGetAction(),
			testName: "When a config map name is specified, it returns one RBAC with 'get' verb on the config map",
			target:   myConfigMapTarget,
			expectedRBACRules: []backplane.RBACRule{
				{
					APIGroups:     []string{""},
					Resources:     []string{"configmaps"},
					ResourceNames: []string{"my-config"},
					Verbs:         []string{"get"},
				},
			},
		},
		{
			action:   NewGetAction(),
			testName: "When a config map name in a different namespace is specified, it returns the same RBAC with 'get' verb on the config map",
			target: ResourceTarget{
				Group:     "",
				Version:   "v1",
				Resource:  "configmaps",
				Namespace: "openshift-logging",
				Name:      "my-config",
			},
			expectedRBACRules: []backplane.RBACRule{
				{
					APIGroups:     []string{""},
					Resources:     []string{"configmaps"},
					ResourceNames: []string{"my-config"},
					Verbs:         []string{"get"},
				},
			},
		},
		{
			action:   NewGetAction(),
			testName: "When a deployment name is specified, it returns one RBAC with 'get' verb on the deployment",
			target: ResourceTarget{
				Group:     "apps",
				Version:   "v1",
				Resource:  "deployments",
				Namespace: "openshift-monitoring",
				Name:      "prometheus",
			},
			expectedRBACRules: []backplane.RBACRule{
				{
					APIGroups:     []string{"apps"},
					Resources:     []string{"deployments"},
					ResourceNames: []string{"prometheus"},
					Verbs:         []string{"get"},
				},
			},
		},
		{
			action:   NewGetAction(),
			testName: "When requesting config maps without specifying a name, it returns one RBAC with 'list' verb",
			target: ResourceTarget{
				Group:     "",
				Version:   "v1",
				Resource:  "configmaps",
				Namespace: "openshift-monitoring",
			},
			expectedRBACRules: []backplane.RBACRule{
				{
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
					Verbs:     []string{"list"},
				},
			},
		},
		// PATCH action tests
		{
			action:   NewPatchAction(),
			testName: "When a config map name is specified, it returns one RBAC with 'patch' verb on the config map",
			target:   myConfigMapTarget,
			expectedRBACRules: []backplane.RBACRule{
				{
					APIGroups:     []string{""},
					Resources:     []string{"configmaps"},
					ResourceNames: []string{"my-config"},
					Verbs:         []string{"patch"},
				},
			},
		},
		// DELETE action tests
		{
			action:   NewDeleteAction(),
			testName: "When a config map name is specified, it returns one RBAC with 'delete' verb on the config map",
			target:   myConfigMapTarget,
			expectedRBACRules: []backplane.RBACRule{
				{
					APIGroups:     []string{""},
					Resources:     []string{"configmaps"},
					ResourceNames: []string{"my-config"},
					Verbs:         []string{"delete"},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("When using a %s action", test.action.Name()), func(t *testing.T) {
			t.Run(test.testName, func(t *testing.T) {
				g := NewWithT(t)
				rules := test.action.RequiredRBAC(test.target)
				g.Expect(rules).To(Equal(test.expectedRBACRules))
			})
		})
	}
}

func TestAction_Execute(t *testing.T) {
	tests := []struct {
		action                      Action
		testName                    string
		target                      ResourceTarget
		params                      map[string]string
		clientObjects               []runtime.Object
		clientObjectsAfterExecution []runtime.Object
		expectedResultResources     []unstructured.Unstructured
		isExpectingError            bool
	}{
		// GET action tests
		{
			action:                      NewGetAction(),
			testName:                    "When the requested config map is known by the client, it returns the config map",
			target:                      myConfigMapTarget,
			clientObjects:               []runtime.Object{myConfigMap},
			clientObjectsAfterExecution: []runtime.Object{myConfigMap},
			expectedResultResources:     []unstructured.Unstructured{*myConfigMap},
		},
		{
			action:                      NewGetAction(),
			testName:                    "When the requested secret is known by the client, it returns the secret",
			target:                      mySecretTarget,
			clientObjects:               []runtime.Object{mySecret},
			clientObjectsAfterExecution: []runtime.Object{mySecret},
			expectedResultResources:     []unstructured.Unstructured{*mySecret},
		},
		{
			action:                      NewGetAction(),
			testName:                    "When the requested node is known by the client, it returns the node",
			target:                      myNodeTarget,
			clientObjects:               []runtime.Object{myNode},
			clientObjectsAfterExecution: []runtime.Object{myNode},
			expectedResultResources:     []unstructured.Unstructured{*myNode},
		},
		{
			action:           NewGetAction(),
			testName:         "When the requested config map does not exist, it returns an error",
			target:           myConfigMapTarget,
			isExpectingError: true,
		},
		{
			action:                      NewGetAction(),
			testName:                    "When the requested config map is in some other namespace, it returns an error",
			target:                      myConfigMapTarget,
			clientObjects:               []runtime.Object{newConfigMap("openshift-logging", "my-config", "my-config-key", "my-config-value")},
			clientObjectsAfterExecution: []runtime.Object{newConfigMap("openshift-logging", "my-config", "my-config-key", "my-config-value")},
			isExpectingError:            true,
		},
		{
			action:   NewGetAction(),
			testName: "When requesting config maps without specifying a name, it returns the config maps in the requested namespace known by the client",
			target: ResourceTarget{
				Group:     "",
				Version:   "v1",
				Resource:  "configmaps",
				Namespace: "openshift-monitoring",
			},
			clientObjects:               []runtime.Object{cm1, cm2},
			clientObjectsAfterExecution: []runtime.Object{cm1, cm2},
			expectedResultResources:     []unstructured.Unstructured{*cm1, *cm2},
		},
		{
			action:   NewGetAction(),
			testName: "When requesting config maps without specifying a name and when there is no config maps in the namespace, it returns nothing",
			target: ResourceTarget{
				Group:     "",
				Version:   "v1",
				Resource:  "configmaps",
				Namespace: "openshift-monitoring",
			},
			clientObjects:               []runtime.Object{newConfigMap("openshift-logging", "some-config", "some-config-key", "some-config-value")}, // fake client needs at least one config map somewhere to work, so we add one in a different namespace
			clientObjectsAfterExecution: []runtime.Object{newConfigMap("openshift-logging", "some-config", "some-config-key", "some-config-value")},
		},
		{
			action:   NewGetAction(),
			testName: "When requesting config maps without specifying a name and the namespace, it returns an error",
			target: ResourceTarget{
				Group:    "",
				Version:  "v1",
				Resource: "configmaps",
			},
			clientObjects:               []runtime.Object{cm1, cm2},
			clientObjectsAfterExecution: []runtime.Object{cm1, cm2},
			isExpectingError:            true,
		},
		{
			action:   NewGetAction(),
			testName: "When requesting nodes without specifying a name and a namespace, it returns the nodes known by the client",
			target: ResourceTarget{
				Group:         "",
				Version:       "v1",
				Resource:      "nodes",
				ClusterScoped: true,
			},
			clientObjects:               []runtime.Object{n1, n2},
			clientObjectsAfterExecution: []runtime.Object{n1, n2},
			expectedResultResources:     []unstructured.Unstructured{*n1, *n2},
		},
		{
			action:   NewGetAction(),
			testName: "When requesting nodes without specifying a name but specifying a namespace, it returns an error",
			target: ResourceTarget{
				Group:         "",
				Version:       "v1",
				Resource:      "nodes",
				ClusterScoped: true,
				Namespace:     "openshift-monitoring",
			},
			clientObjects:               []runtime.Object{n1, n2},
			clientObjectsAfterExecution: []runtime.Object{n1, n2},
			isExpectingError:            true,
		},
		// PATCH action tests
		{
			action:                      NewPatchAction(),
			testName:                    "When the requested config map is known by the client, it patches and returns the config map with the updated field",
			target:                      myConfigMapTarget,
			params:                      map[string]string{"patch": `{"data":{"my-config-key":"updated"}}`},
			clientObjects:               []runtime.Object{myConfigMap},
			clientObjectsAfterExecution: []runtime.Object{newConfigMap("openshift-monitoring", "my-config", "my-config-key", "updated")},
			expectedResultResources:     []unstructured.Unstructured{*newConfigMap("openshift-monitoring", "my-config", "my-config-key", "updated")},
		},
		{
			action:                      NewPatchAction(),
			testName:                    "When the requested secret is known by the client, it patches and returns the secret with the updated field",
			target:                      mySecretTarget,
			params:                      map[string]string{"patch": `{"data":{"my-secret-key":"updated"}}`},
			clientObjects:               []runtime.Object{mySecret},
			clientObjectsAfterExecution: []runtime.Object{newSecret("openshift-monitoring", "my-secret", "my-secret-key", "updated")},
			expectedResultResources:     []unstructured.Unstructured{*newSecret("openshift-monitoring", "my-secret", "my-secret-key", "updated")},
		},
		{
			action:                      NewPatchAction(),
			testName:                    "When the requested node is known by the client, it patches and returns the node with the updated field",
			target:                      myNodeTarget,
			params:                      map[string]string{"patch": `{"metadata":{"labels":{"test":"value"}}}`},
			clientObjects:               []runtime.Object{myNode},
			clientObjectsAfterExecution: []runtime.Object{newNode("my-node", map[string]interface{}{"test": "value"})},
			expectedResultResources:     []unstructured.Unstructured{*newNode("my-node", map[string]interface{}{"test": "value"})},
		},
		{
			action:           NewPatchAction(),
			testName:         "When the requested config map does not exist, it returns an error",
			target:           myConfigMapTarget,
			params:           map[string]string{"patch": `{"data":{"my-config-key":"updated"}}`},
			isExpectingError: true,
		},
		{
			action:                      NewPatchAction(),
			testName:                    "When the requested config map is in some other namespace, it returns an error",
			target:                      myConfigMapTarget,
			params:                      map[string]string{"patch": `{"data":{"my-config-key":"updated"}}`},
			clientObjects:               []runtime.Object{newConfigMap("openshift-logging", "my-config", "my-config-key", "my-config-value")},
			clientObjectsAfterExecution: []runtime.Object{newConfigMap("openshift-logging", "my-config", "my-config-key", "my-config-value")},
			isExpectingError:            true,
		},
		{
			action:   NewPatchAction(),
			testName: "When the target does not specify a name, it returns an error",
			target: ResourceTarget{
				Group:     "",
				Version:   "v1",
				Resource:  "configmaps",
				Namespace: "openshift-monitoring",
			},
			params:                      map[string]string{"patch": `{"data":{"my-config-key":"updated"}}`},
			clientObjects:               []runtime.Object{cm1, cm2},
			clientObjectsAfterExecution: []runtime.Object{cm1, cm2},
			isExpectingError:            true,
		},
		{
			action:                      NewPatchAction(),
			testName:                    "When there are some other config maps known by the client, it patches the requested config map only and returns it",
			target:                      myConfigMapTarget,
			params:                      map[string]string{"patch": `{"data":{"my-config-key":"updated"}}`},
			clientObjects:               []runtime.Object{myConfigMap, cm1, cm2, mySecret},
			clientObjectsAfterExecution: []runtime.Object{newConfigMap("openshift-monitoring", "my-config", "my-config-key", "updated"), cm1, cm2, mySecret},
			expectedResultResources:     []unstructured.Unstructured{*newConfigMap("openshift-monitoring", "my-config", "my-config-key", "updated")},
		},
		{
			action:                      NewPatchAction(),
			testName:                    "When the requested config map is known by the client but the patch is empty, it returns the config map but does nothing",
			target:                      myConfigMapTarget,
			params:                      map[string]string{"patch": `{}`},
			clientObjects:               []runtime.Object{myConfigMap},
			clientObjectsAfterExecution: []runtime.Object{myConfigMap},
			expectedResultResources:     []unstructured.Unstructured{*myConfigMap},
		},
		{
			action:                      NewPatchAction(),
			testName:                    "When the requested config map is known by the client but the request 'patch' param is missing, it returns an error",
			target:                      myConfigMapTarget,
			clientObjects:               []runtime.Object{myConfigMap},
			clientObjectsAfterExecution: []runtime.Object{myConfigMap},
			isExpectingError:            true,
		},
		{
			action:        NewPatchAction(),
			testName:      "When the requested config map is known by the client and the patch adds new field, it patches and returns the config map with the new field",
			target:        myConfigMapTarget,
			params:        map[string]string{"patch": `{"data":{"my-new-config-key":"some-new-value"}}`},
			clientObjects: []runtime.Object{myConfigMap},
			clientObjectsAfterExecution: []runtime.Object{newDataObject("ConfigMap", "openshift-monitoring", "my-config", map[string]interface{}{
				"my-config-key":     "my-config-value",
				"my-new-config-key": "some-new-value"})},
			expectedResultResources: []unstructured.Unstructured{*newDataObject("ConfigMap", "openshift-monitoring", "my-config", map[string]interface{}{
				"my-config-key":     "my-config-value",
				"my-new-config-key": "some-new-value"})},
		},
		{
			action:                      NewPatchAction(),
			testName:                    "When the requested config map is known by the client and the patch deletes the existing field, it patches and returns the config map without the field",
			target:                      myConfigMapTarget,
			params:                      map[string]string{"patch": `{"data":{"my-config-key":null}}`},
			clientObjects:               []runtime.Object{myConfigMap},
			clientObjectsAfterExecution: []runtime.Object{newDataObject("ConfigMap", "openshift-monitoring", "my-config", map[string]interface{}{})},
			expectedResultResources:     []unstructured.Unstructured{*newDataObject("ConfigMap", "openshift-monitoring", "my-config", map[string]interface{}{})},
		},
		// DELETE action tests
		{
			action:                      NewDeleteAction(),
			testName:                    "When the requested config map is known by the client, it deletes the config map",
			target:                      myConfigMapTarget,
			clientObjects:               []runtime.Object{myConfigMap},
			clientObjectsAfterExecution: []runtime.Object{},
		},
		{
			action:                      NewDeleteAction(),
			testName:                    "When the requested secret is known by the client, it deletes the secret",
			target:                      mySecretTarget,
			clientObjects:               []runtime.Object{mySecret},
			clientObjectsAfterExecution: []runtime.Object{},
		},
		{
			action:                      NewDeleteAction(),
			testName:                    "When the requested node is known by the client, it deletes the node",
			target:                      myNodeTarget,
			clientObjects:               []runtime.Object{myNode},
			clientObjectsAfterExecution: []runtime.Object{},
		},
		{
			action:                      NewDeleteAction(),
			testName:                    "When there are some other config maps known by the client, it only deletes the requested config map",
			target:                      myConfigMapTarget,
			clientObjects:               []runtime.Object{cm1, cm2, mySecret, myConfigMap},
			clientObjectsAfterExecution: []runtime.Object{cm1, cm2, mySecret},
		},
		{
			action:                      NewDeleteAction(),
			testName:                    "When the requested config map does not exist, it returns an error",
			target:                      myConfigMapTarget,
			clientObjects:               []runtime.Object{cm1, cm2, mySecret},
			clientObjectsAfterExecution: []runtime.Object{cm1, cm2, mySecret},
			isExpectingError:            true,
		},
		{
			action:   NewDeleteAction(),
			testName: "When the target does not specify a name, it returns an error",
			target: ResourceTarget{
				Group:     "",
				Version:   "v1",
				Resource:  "configmaps",
				Namespace: "openshift-monitoring",
			},
			clientObjects:               []runtime.Object{myConfigMap},
			clientObjectsAfterExecution: []runtime.Object{myConfigMap},
			isExpectingError:            true,
		},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("When using a %s action", test.action.Name()), func(t *testing.T) {
			t.Run(test.testName, func(t *testing.T) {
				g := NewWithT(t)

				client := newFakeClient(test.clientObjects...)
				result, err := test.action.Execute(context.Background(), Clients{Dynamic: client}, ActionRequest{
					Target: test.target,
					Params: test.params,
				})
				if test.isExpectingError {
					g.Expect(err).To(HaveOccurred())
					g.Expect(result).To(BeNil())
				} else {
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(result).ToNot(BeNil())
					g.Expect(result.Message).ToNot(BeEmpty())
					g.Expect(result.Resources).To(ConsistOf(test.expectedResultResources))
				}

				// Making sure that the objects know by the client are the expected ones
				{
					gvkNsToExpectedObjects := map[struct {
						gvk schema.GroupVersionKind
						ns  string
					}]*[]runtime.Object{}

					for _, expectedObj := range test.clientObjectsAfterExecution {
						accessor, err := meta.Accessor(expectedObj)
						g.Expect(err).ToNot(HaveOccurred())

						gvkNs := struct {
							gvk schema.GroupVersionKind
							ns  string
						}{expectedObj.GetObjectKind().GroupVersionKind(), accessor.GetNamespace()}
						if _, exists := gvkNsToExpectedObjects[gvkNs]; !exists {
							gvkNsToExpectedObjects[gvkNs] = &[]runtime.Object{}
						}

						expectedObjects := gvkNsToExpectedObjects[gvkNs]
						*expectedObjects = append(*expectedObjects, expectedObj)
					}

					for gvkNs, expectedObjects := range gvkNsToExpectedObjects {
						mapping, err := mapper.RESTMapping(gvkNs.gvk.GroupKind(), gvkNs.gvk.Version)
						g.Expect(err).ToNot(HaveOccurred())
						gvr := mapping.Resource

						objectsListObject, err := client.Tracker().List(gvr, gvkNs.gvk, gvkNs.ns)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(objectsListObject).ToNot(BeNil())

						objectsList, ok := objectsListObject.(*unstructured.UnstructuredList)
						g.Expect(ok).To(BeTrue(), "expected UnstructuredList, got %T", objectsListObject)

						objects := []runtime.Object{}
						for _, obj := range objectsList.Items {
							objects = append(objects, &obj)
						}
						g.Expect(objects).To(ConsistOf(*expectedObjects))
					}
				}
			})
		})
	}
}

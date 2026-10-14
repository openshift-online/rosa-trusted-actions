package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
)

// Target identifies what an action acts upon. Each execution scope defines its
// own implementation; the unexported marker keeps executor requests from
// accepting values that aren't a target of some scope.
type Target interface {
	isTarget()
}

type ResourceTarget struct {
	Group         string `json:"group"`
	Version       string `json:"version"`
	Resource      string `json:"resource"`
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	ClusterScoped bool   `json:"clusterScoped"`
}

func (ResourceTarget) isTarget() {}

type KubeClients struct {
	Dynamic     dynamic.Interface
	PodExecutor backplane.PodExecutor
}

type KubeActionRequest struct {
	Target         ResourceTarget
	ClusterVersion string
	Params         map[string]string
}

// AWSTarget identifies the AWS objects an action acts on. ID is empty when the
// action should list rather than fetch one object, mirroring how an empty
// ResourceTarget.Name means "list".
type AWSTarget struct {
	Region   string `json:"region"`
	Resource string `json:"resource"`
	ID       string `json:"id"`
	// InfraID is the target cluster's own infrastructure ID, resolved from OCM
	// alongside the region. Only write actions carry one: it is what an
	// instance's cluster tag is checked against before anything is changed, so
	// that reaching a cluster's AWS account does not amount to being allowed to
	// act on every instance that happens to live in it.
	InfraID string `json:"infra_id,omitempty"`
}

func (AWSTarget) isTarget() {}

type AWSActionRequest struct {
	Target AWSTarget
	Params map[string]string
}

type ActionResult struct {
	Resources []map[string]interface{}
	Message   string
}

// Action is the scope-independent identity of an action. It deliberately has
// no Execute method: running an action requires its scope-specific subtype
// (KubeAction or AWSAction), so an action cannot be executed without going
// through the scope that authorizes it and provisions its clients.
type Action interface {
	Name() string
}

// KubeAction is implemented by actions that run against a cluster's
// Kubernetes API.
type KubeAction interface {
	Action
	RequiredRBAC(target ResourceTarget) []backplane.RBACRule
	UsesPodExec() bool
	Execute(ctx context.Context, clients KubeClients, req KubeActionRequest) (*ActionResult, error)
}

// AWSAction is implemented by actions that run against the AWS API.
// RequiredServices is the least-privilege counterpart of KubeAction's
// RequiredRBAC: only the clients it names are built for the execution.
type AWSAction interface {
	Action
	RequiredServices(target AWSTarget) []awsapi.Service
	Execute(ctx context.Context, clients awsapi.Clients, req AWSActionRequest) (*ActionResult, error)
}

// AWS write operations, the values the server's write allowlist is configured
// with. They are named after what they do to the instance rather than after
// the AWS API call, because an operator deciding whether to permit one is
// deciding about the effect, not about the SDK.
const (
	AWSOperationReboot    = "reboot"
	AWSOperationTerminate = "terminate"
)

// AWSWriteAction is implemented by AWS actions that change something. The two
// extra methods are what the scope needs before it can run one and cannot work
// out for itself:
//
//   - Operation names the mutation, and is what the server's write allowlist is
//     checked against. It comes from the action, never from the request, so a
//     caller cannot describe a terminate as a reboot to land on a permitted
//     entry — or omit it entirely and be judged as a read.
//   - Resource fixes the kind of object acted on. A read lets the caller name
//     the resource because listing the wrong kind is harmless; a write must not,
//     or "terminate" would mean whatever the caller decided it meant.
type AWSWriteAction interface {
	AWSAction
	Operation() string
	Resource() string
}

var nameToActionFactory = map[string]func() Action{}

type ActionFactory[A Action] = func() A

func RegisterActionFactory[A Action](actionFactory ActionFactory[A]) {
	action := actionFactory()

	nameToActionFactory[action.Name()] = func() Action {
		action := actionFactory()

		return action
	}
}

func CreateAction(actionName string) Action {
	actionFactory := nameToActionFactory[actionName]
	if actionFactory == nil {
		return nil
	}
	return actionFactory()
}

func GetActionsNames() []string {
	actionNames := []string{}
	for actionName := range nameToActionFactory {
		actionNames = append(actionNames, actionName)
	}
	sort.Strings(actionNames)

	return actionNames
}

// resourcesFrom converts unstructured objects into the scope-independent
// representation carried by ActionResult.
func resourcesFrom(objects ...unstructured.Unstructured) []map[string]interface{} {
	resources := make([]map[string]interface{}, 0, len(objects))
	for _, object := range objects {
		resources = append(resources, object.Object)
	}
	return resources
}

// awsResourcesFrom converts AWS SDK structs into the same representation.
// The SDK types carry no JSON tags, so keys come out as the API's own field
// names (InstanceId, State, ...) — the shape the AWS CLI prints. Fields the
// API did not set are dropped rather than serialized as null, since a nil
// pointer in the SDK means "absent".
func awsResourcesFrom[T any](objects []T) ([]map[string]interface{}, error) {
	resources := make([]map[string]interface{}, 0, len(objects))
	for _, object := range objects {
		raw, err := json.Marshal(object)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize AWS object: %w", err)
		}
		resource := map[string]interface{}{}
		if err := json.Unmarshal(raw, &resource); err != nil {
			return nil, fmt.Errorf("failed to convert AWS object: %w", err)
		}
		pruneUnset(resource)
		resources = append(resources, resource)
	}
	return resources, nil
}

// pruneUnset deletes the nil-valued keys from object and from everything
// nested inside it, so an AWS object's unset fields do not reach the caller as
// a wall of nulls.
//
// It edits in place and returns nothing. The alternative — building a pruned
// copy — would allocate a second map for every nested object of every
// resource in a listing, to no purpose: the only maps it is ever given come
// straight out of json.Unmarshal in awsResourcesFrom and are owned by it.
// Returning the map as well as mutating it would just leave callers unsure
// which of the two was the real effect.
func pruneUnset(object map[string]interface{}) {
	for key, value := range object {
		switch typed := value.(type) {
		case nil:
			delete(object, key)
		case map[string]interface{}:
			pruneUnset(typed)
		case []interface{}:
			pruneUnsetList(typed)
		}
	}
}

// pruneUnsetList is the slice half of pruneUnset, and edits in place for the
// same reason. Nils are not removed from a list: an element's position can be
// meaningful, so a missing one is left as null rather than silently shifting
// everything after it.
func pruneUnsetList(values []interface{}) {
	for _, value := range values {
		switch typed := value.(type) {
		case map[string]interface{}:
			pruneUnset(typed)
		case []interface{}:
			pruneUnsetList(typed)
		}
	}
}

func resourceClient(clients KubeClients, gvr schema.GroupVersionResource, target ResourceTarget) (dynamic.ResourceInterface, error) {
	if target.ClusterScoped {
		if target.Namespace != "" {
			return nil, fmt.Errorf("namespace must be empty for cluster-scoped resource %s", target.Resource)
		}
		return clients.Dynamic.Resource(gvr), nil
	}
	if target.Namespace == "" {
		return nil, fmt.Errorf("namespace is required for namespaced resource %s", target.Resource)
	}
	return clients.Dynamic.Resource(gvr).Namespace(target.Namespace), nil
}

func scopeLabel(namespace string) string {
	if namespace == "" {
		return "cluster scope"
	}
	return namespace
}

func setIfPresent(m map[string]interface{}, key string, value interface{}) {
	if value != nil {
		m[key] = value
	}
}

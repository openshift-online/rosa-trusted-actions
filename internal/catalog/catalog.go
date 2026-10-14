package catalog

import (
	"strings"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/auth"
	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

type ParamDefinition struct {
	Name        string
	Description string
	Required    bool
	Default     string
}

type ActionDefinition struct {
	Name                 string
	Description          string
	Type                 openapi.ActionType
	Scope                openapi.Scope
	AllowedRoles         []string
	Params               []ParamDefinition
	DryRunAction         string
	WriteCooldownSeconds int
	Approval             string
}

type Catalog struct {
	actions map[string]*ActionDefinition
	order   []string
}

var _ auth.ActionCatalog = (*Catalog)(nil)

func New() *Catalog {
	allRoles := []string{"SREP", "ConfigurationAnomalyDetection", "ROSAAiAgent"}

	c := &Catalog{actions: make(map[string]*ActionDefinition)}

	c.register(&ActionDefinition{
		Name:         "get",
		Description:  "Get or list Kubernetes resources",
		Type:         openapi.Read,
		Scope:        openapi.KubeApi,
		AllowedRoles: allRoles,
		Params: []ParamDefinition{
			{Name: "group", Description: "API group (empty for core resources)"},
			{Name: "version", Description: "API version", Required: true, Default: "v1"},
			{Name: "resource", Description: "Resource type (plural, e.g. configmaps)", Required: true},
			{Name: "namespace", Description: "Target namespace (empty for cluster-scoped resources)"},
			{Name: "name", Description: "Resource name (empty to list all)"},
		},
	})

	c.register(&ActionDefinition{
		Name:         "patch",
		Description:  "Patch a Kubernetes resource using JSON merge patch",
		Type:         openapi.Write,
		Scope:        openapi.KubeApi,
		AllowedRoles: allRoles,
		Params: []ParamDefinition{
			{Name: "group", Description: "API group (empty for core resources)"},
			{Name: "version", Description: "API version", Required: true, Default: "v1"},
			{Name: "resource", Description: "Resource type (plural, e.g. configmaps)", Required: true},
			{Name: "namespace", Description: "Target namespace (empty for cluster-scoped resources)"},
			{Name: "name", Description: "Resource name", Required: true},
			{Name: "patch", Description: "JSON merge patch body", Required: true},
		},
	})

	c.register(&ActionDefinition{
		Name:         "delete",
		Description:  "Delete a Kubernetes resource",
		Type:         openapi.Write,
		Scope:        openapi.KubeApi,
		AllowedRoles: allRoles,
		Params: []ParamDefinition{
			{Name: "group", Description: "API group (empty for core resources)"},
			{Name: "version", Description: "API version", Required: true, Default: "v1"},
			{Name: "resource", Description: "Resource type (plural, e.g. configmaps)", Required: true},
			{Name: "namespace", Description: "Target namespace (empty for cluster-scoped resources)"},
			{Name: "name", Description: "Resource name", Required: true},
		},
	})

	c.register(&ActionDefinition{
		Name:         "describe-nodes",
		Description:  "Describe nodes with correlated pods, events, and lease data",
		Type:         openapi.Read,
		Scope:        openapi.KubeApi,
		AllowedRoles: allRoles,
		Params: []ParamDefinition{
			{Name: "name", Description: "Node name (empty to describe all nodes)"},
		},
	})

	c.register(&ActionDefinition{
		Name:         "get-pull-secret-email",
		Description:  "Get the registered email from the cluster pull secret",
		Type:         openapi.Read,
		Scope:        openapi.KubeApi,
		AllowedRoles: allRoles,
	})

	c.register(&ActionDefinition{
		Name:         "list-alerts",
		Description:  "List firing or pending alerts from Prometheus and optionally active silences from Alertmanager",
		Type:         openapi.Read,
		Scope:        openapi.KubeApi,
		AllowedRoles: allRoles,
		Params: []ParamDefinition{
			{Name: "severity", Description: "Filter by severity: critical, warning (empty for both)"},
			{Name: "state", Description: "Alert state: firing, pending, all", Default: "firing"},
			{Name: "silences", Description: "Include active silences: true, false", Default: "false"},
		},
	})

	c.register(&ActionDefinition{
		Name:         "aws-get",
		Description:  "Get or list AWS resources",
		Type:         openapi.Read,
		Scope:        openapi.AwsApi,
		AllowedRoles: allRoles,
		Params: []ParamDefinition{
			// Built from the action's own registry rather than written out here,
			// so adding a resource cannot leave the catalog describing a surface
			// the server no longer has.
			{Name: "resource", Description: "AWS resource type, one of: " + strings.Join(actions.AWSResourceNames(), ", "), Required: true},
			// No region param: reads always target the cluster's own AWS
			// region, which the server resolves from OCM.
			// Which resources take id, filters and since varies, and listing that
			// here would drift as resources are added. The action rejects a param
			// the resource does not accept with a message naming what it does, so
			// the authoritative answer comes from trying it.
			{Name: "id", Description: "Resource ID (empty to list all); some resources require it, some cannot be narrowed by it"},
			{Name: "filters", Description: "Comma-separated Name=Value API filters, e.g. instance-state-name=running (not accepted by every resource)"},
			{Name: "since", Description: "How far back to look, as a Go duration, e.g. 30m or 48h (cloudtrail-events only)", Default: "2h"},
		},
	})

	// The AWS writes. Neither takes a "resource" param the way aws-get does:
	// each acts on EC2 instances and nothing else, so the kind of object is
	// part of the action rather than something the caller chooses.
	//
	// Both are refused unless the server's write allowlist names their
	// operation, which by default it does not. Being listed here means the
	// action exists, not that a given deployment will run it.
	c.register(&ActionDefinition{
		Name:         "aws-reboot",
		Description:  "Reboot one of the cluster's EC2 instances",
		Type:         openapi.Write,
		Scope:        openapi.AwsApi,
		AllowedRoles: allRoles,
		Params: []ParamDefinition{
			{Name: "id", Description: "EC2 instance ID; the instance must be tagged as owned by this cluster", Required: true},
		},
	})

	c.register(&ActionDefinition{
		Name:        "aws-terminate",
		Description: "Terminate one of the cluster's EC2 instances",
		Type:        openapi.Write,
		Scope:       openapi.AwsApi,
		// Narrower than every other action: terminating an instance destroys it,
		// and unlike a reboot there is no state to come back to. Limited to SREP
		// so the irreversible one is not reachable by an automated caller.
		AllowedRoles: []string{"SREP"},
		Params: []ParamDefinition{
			{Name: "id", Description: "EC2 instance ID; the instance must be tagged as owned by this cluster", Required: true},
		},
	})

	return c
}

func (c *Catalog) register(a *ActionDefinition) {
	c.actions[a.Name] = a
	c.order = append(c.order, a.Name)
}

func (c *Catalog) GetAction(name string) (*auth.Action, bool) {
	a, ok := c.actions[name]
	if !ok {
		return nil, false
	}
	return &auth.Action{
		Name:         a.Name,
		Description:  a.Description,
		AllowedRoles: a.AllowedRoles,
	}, true
}

func (c *Catalog) Get(name string) (*ActionDefinition, bool) {
	a, ok := c.actions[name]
	return a, ok
}

func (c *Catalog) All() []*ActionDefinition {
	result := make([]*ActionDefinition, 0, len(c.order))
	for _, name := range c.order {
		result = append(result, c.actions[name])
	}
	return result
}

func (a *ActionDefinition) ToOpenAPISummary() openapi.TrustedActionSummary {
	return openapi.TrustedActionSummary{
		Name:        a.Name,
		Type:        a.Type,
		Scope:       a.Scope,
		Description: a.Description,
	}
}

func (a *ActionDefinition) ToOpenAPIDetail() openapi.TrustedAction {
	ta := openapi.TrustedAction{
		Name:        a.Name,
		Type:        a.Type,
		Scope:       a.Scope,
		Description: a.Description,
	}

	if len(a.Params) > 0 {
		params := make([]openapi.TrustedActionParam, 0, len(a.Params))
		for _, p := range a.Params {
			param := openapi.TrustedActionParam{Name: p.Name}
			if p.Description != "" {
				desc := p.Description
				param.Description = &desc
			}
			if p.Required {
				req := true
				param.Required = &req
			}
			if p.Default != "" {
				def := p.Default
				param.Default = &def
			}
			params = append(params, param)
		}
		ta.Params = &params
	}

	if a.DryRunAction != "" {
		ta.DryRunAction = &a.DryRunAction
	}

	if a.WriteCooldownSeconds > 0 {
		ta.WriteCooldownSeconds = &a.WriteCooldownSeconds
	}

	if a.Approval != "" {
		ta.Authorization = &struct {
			Approval *string `json:"approval,omitempty"`
		}{Approval: &a.Approval}
	}

	return ta
}

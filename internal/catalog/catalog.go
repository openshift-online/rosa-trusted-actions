package catalog

import (
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

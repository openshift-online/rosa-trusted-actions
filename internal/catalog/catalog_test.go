package catalog

import (
	"testing"

	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

func TestCatalog_New(t *testing.T) {
	c := New()

	if len(c.actions) == 0 {
		t.Fatalf("New() registered no actions")
	}
	if len(c.order) != len(c.actions) {
		t.Fatalf("len(order) = %d, len(actions) = %d, want equal", len(c.order), len(c.actions))
	}
	for name, a := range c.actions {
		if name == "" {
			t.Errorf("action registered under empty name")
		}
		if a.Name != name {
			t.Errorf("actions[%q].Name = %q, want %q", name, a.Name, name)
		}
	}
}

func TestCatalog_All(t *testing.T) {
	c := &Catalog{actions: make(map[string]*ActionDefinition)}
	c.register(&ActionDefinition{Name: "a"})
	c.register(&ActionDefinition{Name: "b"})
	c.register(&ActionDefinition{Name: "c"})

	all := c.All()
	if len(all) != len(c.actions) {
		t.Fatalf("len(All()) = %d, want %d", len(all), len(c.actions))
	}
	for _, a := range all {
		want, ok := c.actions[a.Name]
		if !ok {
			t.Errorf("All() returned unexpected action %q", a.Name)
			continue
		}
		if a != want {
			t.Errorf("All() entry for %q = %p, want %p", a.Name, a, want)
		}
	}
}

func TestCatalog_Get(t *testing.T) {
	c := New()

	def, ok := c.Get("get")
	if !ok {
		t.Fatalf("Get(%q) ok = false, want true", "get")
	}
	if def.Name != "get" {
		t.Errorf("Get(%q).Name = %q, want %q", "get", def.Name, "get")
	}

	def, ok = c.Get("nope")
	if ok {
		t.Errorf("Get(%q) ok = true, want false", "nope")
	}
	if def != nil {
		t.Errorf("Get(%q) def = %+v, want nil", "nope", def)
	}
}

func TestCatalog_GetAction(t *testing.T) {
	c := New()

	action, ok := c.GetAction("get")
	if !ok {
		t.Fatalf("GetAction(%q) ok = false, want true", "get")
	}
	if action.Name != "get" {
		t.Errorf("GetAction(%q).Name = %q, want %q", "get", action.Name, "get")
	}
	if len(action.AllowedRoles) == 0 {
		t.Errorf("GetAction(%q).AllowedRoles is empty, want non-empty", "get")
	}

	action, ok = c.GetAction("nope")
	if ok {
		t.Errorf("GetAction(%q) ok = true, want false", "nope")
	}
	if action != nil {
		t.Errorf("GetAction(%q) action = %+v, want nil", "nope", action)
	}
}

func TestActionDefinition_ToOpenAPISummary(t *testing.T) {
	def := &ActionDefinition{
		Name:        "my-action",
		Description: "does a thing",
		Type:        openapi.Write,
		Scope:       openapi.KubeApi,
	}

	summary := def.ToOpenAPISummary()

	if summary.Name != "my-action" {
		t.Errorf("Name = %q, want %q", summary.Name, "my-action")
	}
	if summary.Type != openapi.Write {
		t.Errorf("Type = %q, want %q", summary.Type, openapi.Write)
	}
	if summary.Scope != openapi.KubeApi {
		t.Errorf("Scope = %q, want %q", summary.Scope, openapi.KubeApi)
	}
	if summary.Description != "does a thing" {
		t.Errorf("Description = %q, want %q", summary.Description, "does a thing")
	}
}

func TestActionDefinition_ToOpenAPIDetail_Params(t *testing.T) {
	def := &ActionDefinition{
		Name: "my-action",
		Params: []ParamDefinition{
			{Name: "a"},
			{Name: "b", Description: "d", Required: true, Default: "x"},
		},
	}

	ta := def.ToOpenAPIDetail()

	if ta.Params == nil {
		t.Fatalf("Params = nil, want non-nil")
	}
	params := *ta.Params
	if len(params) != 2 {
		t.Fatalf("len(Params) = %d, want 2", len(params))
	}

	pa := params[0]
	if pa.Name != "a" {
		t.Errorf("Params[0].Name = %q, want %q", pa.Name, "a")
	}
	if pa.Description != nil {
		t.Errorf("Params[0].Description = %v, want nil", *pa.Description)
	}
	if pa.Required != nil {
		t.Errorf("Params[0].Required = %v, want nil", *pa.Required)
	}
	if pa.Default != nil {
		t.Errorf("Params[0].Default = %v, want nil", *pa.Default)
	}

	pb := params[1]
	if pb.Name != "b" {
		t.Errorf("Params[1].Name = %q, want %q", pb.Name, "b")
	}
	if pb.Description == nil || *pb.Description != "d" {
		t.Errorf("Params[1].Description = %v, want %q", pb.Description, "d")
	}
	if pb.Required == nil || *pb.Required != true {
		t.Errorf("Params[1].Required = %v, want true", pb.Required)
	}
	if pb.Default == nil || *pb.Default != "x" {
		t.Errorf("Params[1].Default = %v, want %q", pb.Default, "x")
	}
}

func TestActionDefinition_ToOpenAPIDetail_NoParams(t *testing.T) {
	def := &ActionDefinition{Name: "my-action"}

	ta := def.ToOpenAPIDetail()

	if ta.Params != nil {
		t.Errorf("Params = %v, want nil", ta.Params)
	}
}

func TestActionDefinition_ToOpenAPIDetail_OptionalMetadata(t *testing.T) {
	def := &ActionDefinition{
		Name:                 "my-action",
		DryRunAction:         "dr",
		WriteCooldownSeconds: 5,
		Approval:             "manual",
	}

	ta := def.ToOpenAPIDetail()

	if ta.DryRunAction == nil || *ta.DryRunAction != "dr" {
		t.Errorf("DryRunAction = %v, want %q", ta.DryRunAction, "dr")
	}
	if ta.WriteCooldownSeconds == nil || *ta.WriteCooldownSeconds != 5 {
		t.Errorf("WriteCooldownSeconds = %v, want 5", ta.WriteCooldownSeconds)
	}
	if ta.Authorization == nil || ta.Authorization.Approval == nil || *ta.Authorization.Approval != "manual" {
		t.Errorf("Authorization = %+v, want Approval = %q", ta.Authorization, "manual")
	}

	zero := &ActionDefinition{Name: "zero"}
	zta := zero.ToOpenAPIDetail()

	if zta.DryRunAction != nil {
		t.Errorf("zero DryRunAction = %v, want nil", *zta.DryRunAction)
	}
	if zta.WriteCooldownSeconds != nil {
		t.Errorf("zero WriteCooldownSeconds = %v, want nil", *zta.WriteCooldownSeconds)
	}
	if zta.Authorization != nil {
		t.Errorf("zero Authorization = %+v, want nil", zta.Authorization)
	}
}

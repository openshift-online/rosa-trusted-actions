package catalog

import (
	"strings"
	"testing"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
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

// TestCatalog_CoversEveryRegisteredAction guards the other end of the pipeline
// from TestScopes_CoverEveryRegisteredAction: the catalog is what the API
// exposes and what CreateExecution validates against, so an action registered
// in internal/actions but missing here is simply unreachable over HTTP, with
// nothing to indicate why.
func TestCatalog_CoversEveryRegisteredAction(t *testing.T) {
	c := New()

	names := actions.GetActionsNames()
	if len(names) == 0 {
		t.Fatal("no actions registered")
	}

	for _, name := range names {
		if _, ok := c.Get(name); !ok {
			t.Errorf("action %q is registered but absent from the catalog", name)
		}
	}
}

// The catalog is the only place a caller can learn which AWS resources exist,
// so its list must be the action's list. Deriving it is what keeps the two in
// step; this fails if someone writes the names out by hand again.
func TestCatalog_AWSGetListsEveryResource(t *testing.T) {
	def, ok := New().Get("aws-get")
	if !ok {
		t.Fatal("aws-get is absent from the catalog")
	}

	var description string
	for _, param := range def.Params {
		if param.Name == "resource" {
			description = param.Description
		}
	}
	if description == "" {
		t.Fatal("aws-get has no resource param")
	}

	for _, name := range actions.AWSResourceNames() {
		if !strings.Contains(description, name) {
			t.Errorf("resource %q is readable but not named in the catalog: %q", name, description)
		}
	}
}

// And the reverse: a catalog entry with no implementation is accepted by the
// API and then fails in the worker, long after the caller got a 202.
func TestCatalog_EveryEntryHasAnImplementation(t *testing.T) {
	for _, def := range New().All() {
		if actions.CreateAction(def.Name) == nil {
			t.Errorf("catalog action %q has no registered implementation", def.Name)
		}
	}
}

// The write actions have to be declared as writes. Type is what the API tells
// callers, and an AWS mutation listed as a Read would misdescribe the most
// consequential thing about it.
func TestCatalog_AWSWritesAreTypedAsWrites(t *testing.T) {
	c := New()

	for _, name := range []string{"aws-reboot", "aws-terminate"} {
		def, ok := c.Get(name)
		if !ok {
			t.Fatalf("Get(%q) not found in the catalog", name)
		}
		if def.Type != openapi.Write {
			t.Errorf("%s Type = %v, want Write", name, def.Type)
		}
		if def.Scope != openapi.AwsApi {
			t.Errorf("%s Scope = %v, want AwsApi", name, def.Scope)
		}
		// The instance id is the whole target; without it there is nothing to
		// act on, and a write that defaulted to "all of them" would be a
		// catastrophe rather than a convenience.
		var id *ParamDefinition
		for i := range def.Params {
			if def.Params[i].Name == "id" {
				id = &def.Params[i]
			}
		}
		if id == nil {
			t.Fatalf("%s has no id param", name)
		}
		if !id.Required {
			t.Errorf("%s id param is optional, want required", name)
		}
		// No resource param: the action fixes what it acts on.
		for _, param := range def.Params {
			if param.Name == "resource" {
				t.Errorf("%s exposes a resource param, but a write fixes its own resource", name)
			}
		}
	}
}

// Terminating an instance cannot be undone, so it is reachable by fewer
// callers than everything else. This is the one place that distinction is
// recorded, which makes it the one place it can be lost.
func TestCatalog_TerminateIsRestrictedToSREP(t *testing.T) {
	c := New()

	def, ok := c.Get("aws-terminate")
	if !ok {
		t.Fatal("aws-terminate not found in the catalog")
	}
	if len(def.AllowedRoles) != 1 || def.AllowedRoles[0] != "SREP" {
		t.Errorf("aws-terminate AllowedRoles = %v, want [SREP]", def.AllowedRoles)
	}

	// Reboot is reversible and stays available to the automated callers.
	reboot, ok := c.Get("aws-reboot")
	if !ok {
		t.Fatal("aws-reboot not found in the catalog")
	}
	if len(reboot.AllowedRoles) <= len(def.AllowedRoles) {
		t.Errorf("aws-reboot AllowedRoles = %v, want it to be broader than terminate's", reboot.AllowedRoles)
	}
}

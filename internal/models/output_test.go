package models

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
)

func TestOutputFromActionResult_Nil(t *testing.T) {
	out, err := OutputFromActionResult(nil)

	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if out != nil {
		t.Errorf("out = %v, want nil", out)
	}
}

func TestOutputFromActionResult_WithResources(t *testing.T) {
	result := &actions.ActionResult{
		Message: "ok",
		Resources: []unstructured.Unstructured{
			{Object: map[string]interface{}{"a": 1}},
			{Object: map[string]interface{}{"b": 2}},
		},
	}

	out, err := OutputFromActionResult(result)

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if out.Message != "ok" {
		t.Errorf("Message = %v, want ok", out.Message)
	}
	if len(out.Resources) != 2 {
		t.Fatalf("len(Resources) = %v, want 2", len(out.Resources))
	}
	if out.Resources[0]["a"] != 1 {
		t.Errorf("Resources[0][a] = %v, want 1", out.Resources[0]["a"])
	}
	if out.Resources[1]["b"] != 2 {
		t.Errorf("Resources[1][b] = %v, want 2", out.Resources[1]["b"])
	}
}

func TestOutputFromActionResult_EmptyResources(t *testing.T) {
	result := &actions.ActionResult{Message: "m"}

	out, err := OutputFromActionResult(result)

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(out.Resources) != 0 {
		t.Errorf("len(Resources) = %v, want 0", len(out.Resources))
	}
}

func TestExecutionOutput_ToOpenAPI(t *testing.T) {
	o := &ExecutionOutput{
		Message:   "m",
		Resources: []map[string]interface{}{{"a": 1}},
	}

	out := o.ToOpenAPI()

	if out.Message != o.Message {
		t.Errorf("Message = %v, want %v", out.Message, o.Message)
	}
	if len(out.Resources) != len(o.Resources) {
		t.Fatalf("len(Resources) = %v, want %v", len(out.Resources), len(o.Resources))
	}
	if out.Resources[0]["a"] != 1 {
		t.Errorf("Resources[0][a] = %v, want 1", out.Resources[0]["a"])
	}
}

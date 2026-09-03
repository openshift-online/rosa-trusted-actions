package models

import (
	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

type ExecutionOutput struct {
	Message   string
	Resources []map[string]interface{}
}

func OutputFromActionResult(result *actions.ActionResult) (*ExecutionOutput, error) {
	if result == nil {
		return nil, nil
	}

	resources := []map[string]interface{}{}
	for _, unstructured := range result.Resources {
		resources = append(resources, unstructured.Object)
	}

	return &ExecutionOutput{
		Message:   result.Message,
		Resources: resources,
	}, nil
}

func (o *ExecutionOutput) ToOpenAPI() openapi.ExecutionOutput {
	return openapi.ExecutionOutput{
		Message:   o.Message,
		Resources: o.Resources,
	}
}

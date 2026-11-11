//go:build e2e

package e2e

import "testing"

// AWS reads take no region parameter: the server resolves the target cluster's
// own region from OCM, so a read can only reach the account the cluster lives
// in. That is also why these tests need no fixtures — the cluster's own
// infrastructure is what they read.

func TestAWSGet_EC2Instances(t *testing.T) {
	output := runAction(t, "aws-get", map[string]string{
		"resource": "ec2-instances",
	})

	// Every ROSA cluster runs on EC2 instances, so an empty answer here means
	// the read reached the wrong account or the wrong region, rather than that
	// there was nothing to find.
	if len(output.Resources) == 0 {
		t.Fatal("no EC2 instances returned for the cluster's own account")
	}

	// AWS objects come back under the API's own field names rather than
	// Kubernetes-style ones: the SDK structs carry no JSON tags, so the output
	// is shaped the way the AWS CLI prints it.
	if id, _ := output.Resources[0]["InstanceId"].(string); id == "" {
		t.Errorf("first instance has no InstanceId: %v", output.Resources[0])
	}

	if output.Message == "" {
		t.Error("expected a non-empty message")
	}
}

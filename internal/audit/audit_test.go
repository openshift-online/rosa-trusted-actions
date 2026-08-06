package audit

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
)

func TestMockLogger_LogAllowed(t *testing.T) {
	logger := NewMockLogger(logrus.New())

	logger.Log(Record{
		Timestamp: time.Now(),
		CallerID:  "test-user",
		Action:    "get",
		Target: actions.ResourceTarget{
			Resource:  "configmaps",
			Namespace: "openshift-monitoring",
			Name:      "cluster-config",
		},
		ClusterID: "cluster-123",
		Decision:  DecisionAllowed,
		Outcome:   OutcomeSuccess,
	})

	if len(logger.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(logger.Records))
	}
	if logger.Records[0].Decision != DecisionAllowed {
		t.Errorf("expected decision %q, got %q", DecisionAllowed, logger.Records[0].Decision)
	}
	if logger.Records[0].Outcome != OutcomeSuccess {
		t.Errorf("expected outcome %q, got %q", OutcomeSuccess, logger.Records[0].Outcome)
	}
}

func TestMockLogger_LogDenied(t *testing.T) {
	logger := NewMockLogger(logrus.New())

	logger.Log(Record{
		Timestamp: time.Now(),
		CallerID:  "test-user",
		Action:    "get",
		Target: actions.ResourceTarget{
			Resource:  "secrets",
			Namespace: "openshift-monitoring",
			Name:      "some-secret",
		},
		ClusterID:  "cluster-123",
		Decision:   DecisionDenied,
		DenyReason: "secrets access denied",
		Outcome:    OutcomeSkipped,
	})

	if len(logger.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(logger.Records))
	}
	if logger.Records[0].Decision != DecisionDenied {
		t.Errorf("expected decision %q, got %q", DecisionDenied, logger.Records[0].Decision)
	}
	if logger.Records[0].Outcome != OutcomeSkipped {
		t.Errorf("expected outcome %q, got %q", OutcomeSkipped, logger.Records[0].Outcome)
	}
}

func TestMockLogger_MultipleRecords(t *testing.T) {
	logger := NewMockLogger(logrus.New())

	logger.Log(Record{Action: "get", Decision: DecisionAllowed, Outcome: OutcomeSuccess})
	logger.Log(Record{Action: "delete", Decision: DecisionDenied, Outcome: OutcomeSkipped})

	if len(logger.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(logger.Records))
	}
	if logger.Records[0].Action != "get" {
		t.Errorf("expected first action %q, got %q", "get", logger.Records[0].Action)
	}
	if logger.Records[1].Action != "delete" {
		t.Errorf("expected second action %q, got %q", "delete", logger.Records[1].Action)
	}
}

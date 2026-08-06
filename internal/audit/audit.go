package audit

import (
	"time"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
)

type Decision string

const (
	DecisionAllowed Decision = "allowed"
	DecisionDenied  Decision = "denied"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeSkipped Outcome = "skipped"
)

type Record struct {
	Timestamp  time.Time              `json:"timestamp"`
	CallerID   string                 `json:"caller_id"`
	Action     string                 `json:"action"`
	Target     actions.ResourceTarget `json:"target"`
	ClusterID  string                 `json:"cluster_id"`
	Decision   Decision               `json:"decision"`
	DenyReason string                 `json:"deny_reason,omitempty"`
	Outcome    Outcome                `json:"outcome"`
	Error      string                 `json:"error,omitempty"`
}

type Logger interface {
	Log(record Record)
}

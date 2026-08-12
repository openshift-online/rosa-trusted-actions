package worker

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/models"
)

// NoopRunner marks claimed executions succeeded immediately without doing any
// real work.
//
// TODO: replace with an executor-backed Runner that actually dispatches the
// action (see internal/executor.Executor, currently only wired into
// cmd/action-cli).
type NoopRunner struct {
	Logger *logrus.Logger
}

func NewNoopRunner(logger *logrus.Logger) *NoopRunner {
	return &NoopRunner{Logger: logger}
}

func (r *NoopRunner) Run(ctx context.Context, exec *models.Execution) (string, *time.Time) {
	r.Logger.WithFields(logrus.Fields{
		"execution_id": exec.ID,
		"action":       exec.Action,
	}).Debug("no-op execution (executor not yet wired)")

	now := time.Now().UTC()
	return "succeeded", &now
}

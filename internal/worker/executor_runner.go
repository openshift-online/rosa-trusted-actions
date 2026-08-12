package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/executor"
	"github.com/openshift-online/rosa-trusted-actions/internal/models"
)

// ExecutorRunner runs a claimed execution through internal/executor.Executor,
// the same authorization -> backplane -> action pipeline cmd/action-cli uses.
//
// internal/executor.Executor.Execute runs synchronously and returns once the
// action completes; it does not itself submit a ManifestWork or perform the
// two-phase runner+uploader reconciliation implied by the Execution schema's
// manifest_work_name/runner_seconds/upload_seconds fields. This runner only
// moves that synchronous call off the HTTP request path into the background
// worker pool — it does not implement that two-phase dispatch.
type ExecutorRunner struct {
	logger   *logrus.Logger
	executor *executor.Executor
}

func NewExecutorRunner(logger *logrus.Logger, exec *executor.Executor) *ExecutorRunner {
	return &ExecutorRunner{logger: logger, executor: exec}
}

func (r *ExecutorRunner) Run(ctx context.Context, exec *models.Execution) (string, *time.Time) {
	log := r.logger.WithFields(logrus.Fields{
		"execution_id": exec.ID,
		"action":       exec.Action,
	})

	act, err := resolveAction(exec.Action)
	if err != nil {
		log.WithError(err).Error("resolving action for claimed execution")
		return failedNow(err)
	}

	params, err := decodeParams(exec.Params)
	if err != nil {
		log.WithError(err).Error("decoding params for claimed execution")
		return failedNow(err)
	}

	target := actions.ResourceTarget{
		Group:    params["group"],
		Version:  params["version"],
		Resource: params["resource"],
		Name:     params["name"],
		// Params carries only a flat namespace value (no explicit
		// cluster-scoped flag today) — a resource is cluster-scoped exactly
		// when no namespace was supplied, mirroring the two branches in
		// actions.resourceClient.
		Namespace:     params["namespace"],
		ClusterScoped: params["namespace"] == "",
	}

	callerID := ""
	if exec.Username != nil {
		callerID = *exec.Username
	}

	result := r.executor.Execute(ctx, executor.Request{
		CallerID:  callerID,
		ClusterID: exec.TargetCluster,
		Action:    act,
		Target:    target,
		Params:    params,
	})

	if !result.Allowed {
		log.WithField("reason", result.Reason).Warn("claimed execution denied by authorizer")
		return failedNow(fmt.Errorf("denied: %s", result.Reason))
	}
	if result.Error != nil {
		log.WithError(result.Error).Error("claimed execution failed")
		return failedNow(result.Error)
	}

	now := time.Now().UTC()
	return "succeeded", &now
}

func resolveAction(name string) (actions.Action, error) {
	switch name {
	case "get":
		return actions.NewGetAction(), nil
	case "patch":
		return actions.NewPatchAction(), nil
	case "delete":
		return actions.NewDeleteAction(), nil
	default:
		return nil, fmt.Errorf("unknown action %q", name)
	}
}

func decodeParams(raw *json.RawMessage) (map[string]string, error) {
	if raw == nil {
		return map[string]string{}, nil
	}
	var params map[string]string
	if err := json.Unmarshal(*raw, &params); err != nil {
		return nil, fmt.Errorf("failed to decode execution params: %w", err)
	}
	return params, nil
}

func failedNow(_ error) (string, *time.Time) {
	now := time.Now().UTC()
	return "failed", &now
}

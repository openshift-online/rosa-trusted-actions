// Package worker dequeues pending executions from the store and runs them
// in the background, off the HTTP request path.
package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/models"
	"github.com/openshift-online/rosa-trusted-actions/internal/store"
)

// Result for an execution, terminal status, human-readable reason for the
// status and, if applicable, the action result
type RunResult struct {
	Status string
	Reason string
	Output *actions.ActionResult
}

// Runner executes a single claimed execution
type Runner interface {
	Run(ctx context.Context, exec *models.Execution) RunResult
}

// Pool is an in-process worker pool that claims pending executions from the
// store and runs them. Workers wake immediately on Notify and otherwise fall
// back to polling on pollInterval, so correctness never depends on a notify
// actually arriving (a missed notify, or a row inserted by some other path,
// is still picked up on the next poll).
type Pool struct {
	store        store.Store
	logger       *logrus.Logger
	runner       Runner
	concurrency  int
	pollInterval time.Duration
	notify       chan struct{}
	wg           sync.WaitGroup
}

// defaultPollInterval is used when New is given a non-positive pollInterval
// (matches the config package's ROSA_TA_WORKER_POLL_INTERVAL default).
const defaultPollInterval = 5 * time.Second

// New creates a worker pool. concurrency is clamped to at least 1;
// pollInterval is clamped to defaultPollInterval when non-positive, since
// time.NewTicker panics on a non-positive duration.
func New(s store.Store, logger *logrus.Logger, runner Runner, concurrency int, pollInterval time.Duration) *Pool {
	if concurrency < 1 {
		concurrency = 1
	}
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}
	return &Pool{
		store:        s,
		logger:       logger,
		runner:       runner,
		concurrency:  concurrency,
		pollInterval: pollInterval,
		notify:       make(chan struct{}, 1),
	}
}

// Notify wakes a worker to check for pending work immediately, without
// waiting for the next poll. Safe to call from any goroutine; never blocks.
func (p *Pool) Notify() {
	select {
	case p.notify <- struct{}{}:
	default:
	}
}

// Start launches the worker goroutines and returns immediately. Workers run
// until ctx is cancelled.
func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.concurrency; i++ {
		p.wg.Add(1)
		go p.workerLoop(ctx)
	}
}

// Wait blocks until all worker goroutines have exited.
func (p *Pool) Wait() {
	p.wg.Wait()
}

func (p *Pool) workerLoop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}

		exec, err := p.store.ClaimNextExecution(ctx)
		switch {
		case err == nil:
			p.process(ctx, exec)
			continue
		case errors.Is(err, store.ErrNotFound):
			// Nothing pending — wait below for a notify or the next poll.
		case ctx.Err() != nil:
			return
		default:
			p.logger.WithError(err).Error("claiming next execution")
		}

		select {
		case <-ctx.Done():
			return
		case <-p.notify:
		case <-ticker.C:
		}
	}
}

func (p *Pool) process(ctx context.Context, exec *models.Execution) {
	result := p.runner.Run(ctx, exec)
	status := result.Status
	completedAt := time.Now().UTC()

	// TODO: persist reason alongside status once the store has somewhere to
	// put it (Store.UpdateExecutionStatus / the executions table currently
	// has no failure-reason column, and the OpenAPI-generated Execution type
	// has no field to expose it through either). Surfaced via structured
	// logging for now so it isn't silently dropped.
	if result.Reason != "" {
		p.logger.WithFields(logrus.Fields{
			"execution_id": exec.ID,
			"status":       result.Status,
			"reason":       result.Reason,
		}).Warn("execution completed with failure reason")
	}

	output, err := models.OutputFromActionResult(result.Output)
	if err != nil {
		p.logger.WithError(err).WithField("execution_id", exec.ID).Error("creating execution output data")
		status = "failed"
	}

	// Detached from ctx's cancellation: if ctx is cancelled (e.g. shutdown)
	// right as Run finishes, we've already computed a terminal status and
	// must still persist it — otherwise the execution is stuck "running"
	// forever. Still bounded so this can't hang if the store itself is
	// unresponsive.
	updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := p.store.UpdateExecutionWithResult(updateCtx, exec.ID, status, &completedAt, output); err != nil {
		p.logger.WithError(err).WithField("execution_id", exec.ID).Error("updating execution with result after run")
	}
}

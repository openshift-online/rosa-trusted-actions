// Package worker dequeues pending executions from the store and runs them
// in the background, off the HTTP request path.
package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/models"
	"github.com/openshift-online/rosa-trusted-actions/internal/store"
)

// Runner executes a single claimed execution and reports its terminal status
// (and, if applicable, when it completed).
type Runner interface {
	Run(ctx context.Context, exec *models.Execution) (status string, completedAt *time.Time)
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

// New creates a worker pool. concurrency is clamped to at least 1.
func New(s store.Store, logger *logrus.Logger, runner Runner, concurrency int, pollInterval time.Duration) *Pool {
	if concurrency < 1 {
		concurrency = 1
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
	status, completedAt := p.runner.Run(ctx, exec)
	if err := p.store.UpdateExecutionStatus(ctx, exec.ID, status, completedAt); err != nil {
		p.logger.WithError(err).WithField("execution_id", exec.ID).Error("updating execution status after run")
	}
}

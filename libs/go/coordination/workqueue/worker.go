// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package workqueue

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/requestmeta"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/servicekit"
)

const (
	DefaultPollInterval      = 250 * time.Millisecond
	DefaultLeaseDuration     = 2 * time.Minute
	DefaultHeartbeatInterval = 30 * time.Second
	DefaultBatchSize         = 32
	DefaultConcurrency       = 8
	DefaultFailureDelay      = 5 * time.Second
	MaximumConcurrency       = 1_000
)

type Handler interface {
	Handle(context.Context, Item) (Result, error)
}

type HandlerFunc func(context.Context, Item) (Result, error)

func (function HandlerFunc) Handle(ctx context.Context, item Item) (Result, error) {
	if function == nil {
		return Result{}, ErrWorkerStopped
	}
	return function(ctx, item)
}

type EventKind string

const (
	EventClaimed    EventKind = "claimed"
	EventCompleted  EventKind = "completed"
	EventRetried    EventKind = "retried"
	EventFailed     EventKind = "failed"
	EventClaimLost  EventKind = "claim_lost"
	EventCancelled  EventKind = "cancelled"
	EventPollFailed EventKind = "poll_failed"
)

type Event struct {
	Kind     EventKind
	Record   Record
	At       time.Time
	Duration time.Duration
	Err      error
}

type Observer interface{ ObserveWork(Event) }
type ObserverFunc func(Event)

func (function ObserverFunc) ObserveWork(event Event) {
	if function != nil {
		function(event)
	}
}

type WorkerConfig struct {
	Owner             string
	Queues            []string
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	BatchSize         int
	Concurrency       int
	FailureDelay      time.Duration
}

func (config WorkerConfig) normalized() WorkerConfig {
	if config.PollInterval == 0 {
		config.PollInterval = DefaultPollInterval
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = DefaultLeaseDuration
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if config.BatchSize == 0 {
		config.BatchSize = DefaultBatchSize
	}
	if config.Concurrency == 0 {
		config.Concurrency = DefaultConcurrency
	}
	if config.FailureDelay == 0 {
		config.FailureDelay = DefaultFailureDelay
	}
	return config
}

func (config WorkerConfig) Validate() error {
	config = config.normalized()
	request := ClaimRequest{
		Owner:         config.Owner,
		Queues:        config.Queues,
		Limit:         config.BatchSize,
		LeaseDuration: config.LeaseDuration,
	}
	if err := request.Validate(); err != nil {
		return err
	}
	if config.PollInterval <= 0 ||
		config.HeartbeatInterval <= 0 ||
		config.HeartbeatInterval >= config.LeaseDuration ||
		config.Concurrency <= 0 || config.Concurrency > MaximumConcurrency ||
		config.FailureDelay <= 0 {
		return invalid("invalid_work_config", "invalid work worker configuration", "workqueue.WorkerConfig.Validate")
	}
	return nil
}

type WorkerOption func(*Worker) error

func WithClock(value mcclock.Clock) WorkerOption {
	return func(worker *Worker) error {
		if nilValue(value) {
			return invalid("nil_work_clock", "work clock is required", "workqueue.WithClock")
		}
		worker.clock = value
		return nil
	}
}

func WithRetry(value *retry.Executor) WorkerOption {
	return func(worker *Worker) error {
		if value == nil {
			return invalid("nil_work_retry", "work retry executor is required", "workqueue.WithRetry")
		}
		worker.retry = value
		return nil
	}
}

func WithObserver(value Observer) WorkerOption {
	return func(worker *Worker) error {
		worker.observer = value
		return nil
	}
}

type Worker struct {
	store    Store
	handler  Handler
	config   WorkerConfig
	clock    mcclock.Clock
	retry    *retry.Executor
	observer Observer
	running  atomic.Bool
	ready    atomic.Bool
}

func NewWorker(store Store, handler Handler, config WorkerConfig, options ...WorkerOption) (*Worker, error) {
	if nilValue(store) || nilValue(handler) {
		return nil, invalid("missing_work_dependencies", "work store and handler are required", "workqueue.NewWorker")
	}
	config = config.normalized()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	policy, err := retry.NewPolicy(
		retry.WithMaxAttempts(3),
		retry.WithMaxElapsed(config.PollInterval),
	)
	if err != nil {
		return nil, err
	}
	executor, err := retry.NewExecutor(policy)
	if err != nil {
		return nil, err
	}
	worker := &Worker{
		store:   store,
		handler: handler,
		config:  config,
		clock:   mcclock.RealClock{},
		retry:   executor,
	}
	for _, option := range options {
		if option != nil {
			if err := option(worker); err != nil {
				return nil, err
			}
		}
	}
	return worker, nil
}

func (worker *Worker) Run(ctx context.Context) error {
	if ctx == nil || worker == nil || nilValue(worker.store) || nilValue(worker.handler) || nilValue(worker.clock) || worker.retry == nil {
		return invalid("invalid_work_worker", "work worker is not configured", "workqueue.Worker.Run")
	}
	if !worker.running.CompareAndSwap(false, true) {
		return faults.Wrap(ErrWorkerStopped, faults.CodeFailedPrecondition, "worker already ran", faults.WithReason("work_worker_already_run"), faults.WithOperation("workqueue.Worker.Run"), faults.WithRetryPolicy(faults.NoRetry()))
	}

	semaphore := make(chan struct{}, worker.config.Concurrency)
	var running sync.WaitGroup
	worker.ready.Store(true)
	defer func() {
		worker.ready.Store(false)
		running.Wait()
	}()

	for {
		if ctx.Err() != nil {
			return nil
		}
		available := worker.config.Concurrency - len(semaphore)
		if available <= 0 {
			if err := worker.clock.Sleep(ctx, worker.config.PollInterval); err != nil {
				return nil
			}
			continue
		}
		limit := worker.config.BatchSize
		if limit > available {
			limit = available
		}
		request := ClaimRequest{
			Owner:         worker.config.Owner,
			Queues:        worker.config.Queues,
			Limit:         limit,
			LeaseDuration: worker.config.LeaseDuration,
		}
		var claims []Claim
		_, err := worker.retry.Do(ctx, "workqueue.claim", func(operationCtx context.Context, _ retry.Attempt) error {
			var claimErr error
			claims, claimErr = worker.store.Claim(operationCtx, request)
			return claimErr
		})
		if err != nil {
			worker.observe(Event{Kind: EventPollFailed, At: worker.clock.Now().UTC(), Err: err})
			if sleepErr := worker.clock.Sleep(ctx, worker.config.PollInterval); sleepErr != nil {
				return nil
			}
			continue
		}
		if len(claims) == 0 {
			if err := worker.clock.Sleep(ctx, worker.config.PollInterval); err != nil {
				return nil
			}
			continue
		}
		for _, claim := range claims {
			if ctx.Err() != nil {
				break
			}
			semaphore <- struct{}{}
			running.Add(1)
			go func(claim Claim) {
				defer func() {
					<-semaphore
					running.Done()
				}()
				worker.process(ctx, claim)
			}(claim)
		}
	}
}

func (worker *Worker) process(parent context.Context, initial Claim) {
	started := worker.clock.Now().UTC()
	worker.observe(Event{Kind: EventClaimed, Record: initial.Record.Clone(), At: started})

	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)

	var mu sync.RWMutex
	current := initial
	claimLost := make(chan error, 1)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := worker.clock.NewTicker(worker.config.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C():
				mu.RLock()
				claim := current
				mu.RUnlock()
				renewed, err := worker.store.Renew(ctx, claim, worker.config.LeaseDuration)
				if err != nil {
					select {
					case claimLost <- err:
					default:
					}
					cancel(err)
					return
				}
				mu.Lock()
				current = renewed
				mu.Unlock()
			}
		}
	}()

	handlerCtx, handlerErr := requestmeta.WithMetadata(ctx, initial.Record.Item.Request)
	var result Result
	if handlerErr == nil {
		result, handlerErr = worker.handler.Handle(handlerCtx, initial.Item())
	}
	cancel(handlerErr)
	<-heartbeatDone

	mu.RLock()
	finalClaim := current
	mu.RUnlock()

	select {
	case lostErr := <-claimLost:
		worker.observe(Event{Kind: EventClaimLost, Record: finalClaim.Record.Clone(), At: worker.clock.Now().UTC(), Duration: worker.clock.Since(started), Err: lostErr})
		return
	default:
	}

	// Shutdown or preemption leaves the claim leased. It will be reclaimed only
	// after expiry, preventing a second worker from racing a partially stopped
	// handler and preserving at-least-once semantics.
	if parent.Err() != nil {
		worker.observe(Event{Kind: EventCancelled, Record: finalClaim.Record.Clone(), At: worker.clock.Now().UTC(), Duration: worker.clock.Since(started), Err: parent.Err()})
		return
	}

	now := worker.clock.Now().UTC()
	if handlerErr == nil {
		if err := result.Validate(); err != nil {
			handlerErr = err
		} else if err := worker.store.Complete(parent, finalClaim, result, now); err == nil {
			worker.observe(Event{Kind: EventCompleted, Record: finalClaim.Record.Clone(), At: now, Duration: worker.clock.Since(started)})
			return
		} else {
			handlerErr = err
		}
	}

	reason := faults.ReasonOf(handlerErr)
	if reason == "" {
		reason = "work_handler_failed"
	}
	terminal := !faults.IsRetryable(handlerErr) || finalClaim.Record.Attempts >= finalClaim.Record.Item.MaxAttempts
	failure := Failure{Reason: reason, Terminal: terminal}
	if !terminal {
		delay := faults.RetryPolicyOf(handlerErr).Normalized().After
		if delay <= 0 {
			delay = worker.config.FailureDelay
		}
		failure.RetryAt = now.Add(delay)
	}
	storeErr := worker.store.Fail(parent, finalClaim, failure, now)
	combined := errors.Join(handlerErr, storeErr)
	kind := EventRetried
	if terminal || storeErr != nil {
		kind = EventFailed
	}
	worker.observe(Event{Kind: kind, Record: finalClaim.Record.Clone(), At: now, Duration: worker.clock.Since(started), Err: combined})
}

func (worker *Worker) Component(name string) servicekit.Component {
	return servicekit.Component{
		Name: name,
		Run:  worker.Run,
		Liveness: func(context.Context) error {
			if worker == nil {
				return ErrWorkerStopped
			}
			return nil
		},
		Readiness: func(context.Context) error {
			if worker != nil && worker.ready.Load() {
				return nil
			}
			return faults.Wrap(ErrWorkerStopped, faults.CodeUnavailable, "work worker is not ready", faults.WithReason("work_worker_not_ready"), faults.WithRetryPolicy(faults.ImmediateRetry(0)))
		},
	}
}

func (worker *Worker) observe(event Event) {
	if worker == nil || nilValue(worker.observer) {
		return
	}
	defer func() { _ = recover() }()
	worker.observer.ObserveWork(event)
}

func nilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	}
	return false
}

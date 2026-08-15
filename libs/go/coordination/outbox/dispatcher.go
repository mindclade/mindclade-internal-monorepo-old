// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outbox

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"time"

	mcclock "mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/retry"
	"mindclade.internal/libs/go/servicekit"
)

const (
	DefaultPollInterval  = 250 * time.Millisecond
	DefaultClaimDuration = 30 * time.Second
	DefaultBatchSize     = 100
	DefaultMaxDeliveries = 25
)

type DispatchEventKind string

const (
	DispatchClaimed     DispatchEventKind = "claimed"
	DispatchPublished   DispatchEventKind = "published"
	DispatchRescheduled DispatchEventKind = "rescheduled"
	DispatchDeadLetter  DispatchEventKind = "dead_letter"
	DispatchFailed      DispatchEventKind = "failed"
)

type DispatchEvent struct {
	Kind     DispatchEventKind
	Message  Message
	Claim    Claim
	At       time.Time
	Duration time.Duration
	Err      error
}

type DispatchObserver interface{ ObserveDispatch(DispatchEvent) }
type DispatchObserverFunc func(DispatchEvent)

func (function DispatchObserverFunc) ObserveDispatch(event DispatchEvent) {
	if function != nil {
		function(event)
	}
}

type DispatcherConfig struct {
	Owner         string
	Topics        []string
	PollInterval  time.Duration
	ClaimDuration time.Duration
	BatchSize     int
	MaxDeliveries uint32
	IdleReady     bool
}

func (config DispatcherConfig) normalized() DispatcherConfig {
	if config.PollInterval == 0 {
		config.PollInterval = DefaultPollInterval
	}
	if config.ClaimDuration == 0 {
		config.ClaimDuration = DefaultClaimDuration
	}
	if config.BatchSize == 0 {
		config.BatchSize = DefaultBatchSize
	}
	if config.MaxDeliveries == 0 {
		config.MaxDeliveries = DefaultMaxDeliveries
	}
	return config
}

func (config DispatcherConfig) Validate() error {
	config = config.normalized()
	request := ClaimRequest{Owner: config.Owner, Topics: config.Topics, Limit: config.BatchSize, LeaseDuration: config.ClaimDuration}
	if err := request.Validate(); err != nil || config.PollInterval <= 0 || config.MaxDeliveries == 0 || config.MaxDeliveries > MaximumAttempts {
		return faults.Wrap(errors.Join(ErrInvalidRequest, err), faults.CodeInvalidArgument, "invalid outbox dispatcher configuration", faults.WithReason("invalid_outbox_dispatcher_config"), faults.WithOperation("outbox.DispatcherConfig.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

type DispatcherOption func(*Dispatcher) error

func WithDispatcherClock(value mcclock.Clock) DispatcherOption {
	return func(dispatcher *Dispatcher) error {
		if nilInterface(value) {
			return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "dispatcher clock is required", faults.WithReason("nil_outbox_dispatcher_clock"), faults.WithOperation("outbox.WithDispatcherClock"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		dispatcher.clock = value
		return nil
	}
}
func WithDispatchObserver(value DispatchObserver) DispatcherOption {
	return func(dispatcher *Dispatcher) error { dispatcher.observer = value; return nil }
}
func WithPublishRetry(value *retry.Executor) DispatcherOption {
	return func(dispatcher *Dispatcher) error {
		if value == nil {
			return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "publish retry executor is required", faults.WithReason("nil_outbox_retry_executor"), faults.WithOperation("outbox.WithPublishRetry"), faults.WithRetryPolicy(faults.NoRetry()))
		}
		dispatcher.retry = value
		return nil
	}
}

type Dispatcher struct {
	store     Store
	publisher Publisher
	config    DispatcherConfig
	clock     mcclock.Clock
	retry     *retry.Executor
	observer  DispatchObserver
	running   atomic.Bool
	ready     atomic.Bool
}

func NewDispatcher(store Store, publisher Publisher, config DispatcherConfig, options ...DispatcherOption) (*Dispatcher, error) {
	if nilInterface(store) || nilInterface(publisher) {
		return nil, faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "outbox store and publisher are required", faults.WithReason("outbox_dispatcher_dependencies_missing"), faults.WithOperation("outbox.NewDispatcher"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	config = config.normalized()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	policy, _ := retry.NewPolicy(retry.WithMaxAttempts(3), retry.WithMaxElapsed(config.ClaimDuration/2))
	retryExecutor, err := retry.NewExecutor(policy)
	if err != nil {
		return nil, err
	}
	dispatcher := &Dispatcher{store: store, publisher: publisher, config: config, clock: mcclock.RealClock{}, retry: retryExecutor}
	for _, option := range options {
		if option != nil {
			if err := option(dispatcher); err != nil {
				return nil, err
			}
		}
	}
	return dispatcher, nil
}

func (dispatcher *Dispatcher) Run(ctx context.Context) error {
	if ctx == nil {
		return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "dispatcher context is required", faults.WithReason("nil_context"), faults.WithOperation("outbox.Dispatcher.Run"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if dispatcher == nil || nilInterface(dispatcher.store) || nilInterface(dispatcher.publisher) || dispatcher.retry == nil || nilInterface(dispatcher.clock) {
		return faults.Wrap(ErrInvalidRequest, faults.CodeFailedPrecondition, "outbox dispatcher is not configured", faults.WithReason("outbox_dispatcher_missing"), faults.WithOperation("outbox.Dispatcher.Run"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if !dispatcher.running.CompareAndSwap(false, true) {
		return faults.Wrap(ErrDispatcherStopped, faults.CodeFailedPrecondition, "outbox dispatcher is already running or completed", faults.WithReason("outbox_dispatcher_already_run"), faults.WithOperation("outbox.Dispatcher.Run"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	defer dispatcher.ready.Store(false)
	dispatcher.ready.Store(true)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		processed, err := dispatcher.dispatch(ctx)
		if err != nil {
			dispatcher.observe(DispatchEvent{Kind: DispatchFailed, At: dispatcher.clock.Now(), Err: err})
			if sleepErr := dispatcher.clock.Sleep(ctx, dispatcher.config.PollInterval); sleepErr != nil {
				return nil
			}
			continue
		}
		if processed == 0 {
			if sleepErr := dispatcher.clock.Sleep(ctx, dispatcher.config.PollInterval); sleepErr != nil {
				return nil
			}
		}
	}
}

func (dispatcher *Dispatcher) dispatch(ctx context.Context) (int, error) {
	request := ClaimRequest{Owner: dispatcher.config.Owner, Topics: dispatcher.config.Topics, Limit: dispatcher.config.BatchSize, LeaseDuration: dispatcher.config.ClaimDuration}
	var claims []Claim
	_, err := dispatcher.retry.Do(ctx, "outbox.claim", func(ctx context.Context, _ retry.Attempt) error {
		var claimErr error
		claims, claimErr = dispatcher.store.Claim(ctx, request)
		return claimErr
	})
	if err != nil {
		return 0, qualify(err, "outbox.Dispatcher.claim")
	}
	for _, claim := range claims {
		dispatcher.observe(DispatchEvent{Kind: DispatchClaimed, Message: claim.Message(), Claim: claim, At: dispatcher.clock.Now()})
		started := dispatcher.clock.Now()
		_, publishErr := dispatcher.retry.Do(ctx, "outbox.publish", func(ctx context.Context, _ retry.Attempt) error {
			return dispatcher.publisher.Publish(ctx, claim.Message())
		})
		if publishErr == nil {
			markErr := dispatcher.store.MarkPublished(ctx, claim, dispatcher.clock.Now())
			if markErr != nil {
				return len(claims), qualify(markErr, "outbox.Dispatcher.mark_published")
			}
			dispatcher.observe(DispatchEvent{Kind: DispatchPublished, Message: claim.Message(), Claim: claim, At: dispatcher.clock.Now(), Duration: dispatcher.clock.Since(started)})
			continue
		}
		if claim.Attempts() >= dispatcher.config.MaxDeliveries {
			deadErr := dispatcher.store.DeadLetter(ctx, claim, dispatcher.clock.Now(), safeErrorReason(publishErr))
			if deadErr != nil {
				return len(claims), qualify(errors.Join(publishErr, deadErr), "outbox.Dispatcher.dead_letter")
			}
			dispatcher.observe(DispatchEvent{Kind: DispatchDeadLetter, Message: claim.Message(), Claim: claim, At: dispatcher.clock.Now(), Duration: dispatcher.clock.Since(started), Err: publishErr})
			continue
		}
		next := dispatcher.clock.Now().Add(rescheduleDelay(publishErr, claim.Attempts()))
		rescheduleErr := dispatcher.store.Reschedule(ctx, claim, next, safeErrorReason(publishErr))
		if rescheduleErr != nil {
			return len(claims), qualify(errors.Join(publishErr, rescheduleErr), "outbox.Dispatcher.reschedule")
		}
		dispatcher.observe(DispatchEvent{Kind: DispatchRescheduled, Message: claim.Message(), Claim: claim, At: dispatcher.clock.Now(), Duration: dispatcher.clock.Since(started), Err: publishErr})
	}
	return len(claims), nil
}

func (dispatcher *Dispatcher) Component(name string) servicekit.Component {
	return servicekit.Component{
		Name: name,
		Run:  dispatcher.Run,
		Liveness: func(context.Context) error {
			if dispatcher == nil {
				return ErrDispatcherStopped
			}
			return nil
		},
		Readiness: func(context.Context) error {
			if dispatcher != nil && dispatcher.ready.Load() {
				return nil
			}
			return faults.Wrap(ErrDispatcherStopped, faults.CodeUnavailable, "outbox dispatcher is not ready", faults.WithReason("outbox_dispatcher_not_ready"), faults.WithRetryPolicy(faults.ImmediateRetry(0)))
		},
	}
}

func (dispatcher *Dispatcher) observe(event DispatchEvent) {
	if dispatcher == nil || nilInterface(dispatcher.observer) {
		return
	}
	defer func() { _ = recover() }()
	dispatcher.observer.ObserveDispatch(event)
}

func safeErrorReason(err error) string {
	reason := faults.ReasonOf(err)
	if reason == "" {
		reason = ReasonPublishFailed
	}
	if len(reason) > 256 {
		reason = reason[:256]
	}
	return reason
}

func rescheduleDelay(err error, attempts uint32) time.Duration {
	policy := faults.RetryPolicyOf(err).Normalized()
	if policy.After > 0 {
		return policy.After
	}
	shift := attempts
	if shift > 8 {
		shift = 8
	}
	return time.Second * time.Duration(1<<shift)
}

func qualify(err error, operation string) error {
	if err == nil {
		return nil
	}
	code := faults.CodeOf(err)
	if code == faults.CodeUnknown {
		code = faults.CodeUnavailable
	}
	reason := faults.ReasonOf(err)
	if reason == "" {
		reason = ReasonStoreFailed
	}
	policy := faults.RetryPolicyOf(err)
	if !policy.Specified() && code == faults.CodeUnavailable {
		policy = faults.BackoffRetry(0)
	}
	return faults.Wrap(err, code, "outbox operation failed", faults.WithReason(reason), faults.WithOperation(operation), faults.WithRetryPolicy(policy), faults.WithFields(faults.FieldsOf(err)))
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

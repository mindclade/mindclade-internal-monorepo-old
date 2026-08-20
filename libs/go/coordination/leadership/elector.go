// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package leadership

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/retry"
	"go.mindclade.dev/libs/go/servicekit"
	"go.mindclade.dev/libs/go/storage/lease"
)

const (
	DefaultTTL             = 30 * time.Second
	DefaultRenewInterval   = 10 * time.Second
	DefaultAcquireInterval = 2 * time.Second
	DefaultReleaseTimeout  = 5 * time.Second
)

// Session is the immutable leadership epoch passed to a leader handler. Fence
// is the durable lease version and must be included in writes where stale
// leaders could otherwise commit after replacement.
type Session struct {
	Lease lease.Lease
}

func (session Session) Fence() uint64   { return session.Lease.Version }
func (session Session) Owner() string   { return session.Lease.Owner }
func (session Session) Key() lease.Key  { return session.Lease.Key }
func (session Session) Validate() error { return session.Lease.Validate() }

// Handler runs only while leadership is held. It must stop when ctx is
// canceled. A nil return is graceful service completion; a non-nil return
// fails the elector and therefore the owning service.
type Handler func(context.Context, Session) error

type Config struct {
	Key                    lease.Key
	Owner                  string
	TTL                    time.Duration
	RenewInterval          time.Duration
	AcquireInterval        time.Duration
	ReleaseTimeout         time.Duration
	RequireLeaderReadiness bool
	// ExitOnLeadershipLoss makes a lost lease terminal for this process. Use it
	// when the handler owns a single-use runtime such as a workqueue worker,
	// projector, or controller manager: those mechanisms cannot be restarted
	// safely in the same process after their leadership context is canceled.
	ExitOnLeadershipLoss bool
}

func (config Config) normalized() Config {
	config.Owner = strings.TrimSpace(config.Owner)
	if config.TTL == 0 {
		config.TTL = DefaultTTL
	}
	if config.RenewInterval == 0 {
		config.RenewInterval = DefaultRenewInterval
	}
	if config.AcquireInterval == 0 {
		config.AcquireInterval = DefaultAcquireInterval
	}
	if config.ReleaseTimeout == 0 {
		config.ReleaseTimeout = DefaultReleaseTimeout
	}
	return config
}
func (config Config) Validate() error {
	config = config.normalized()
	if err := config.Key.Validate(); err != nil {
		return faults.Wrap(errors.Join(ErrInvalidConfig, err), faults.CodeInvalidArgument, "invalid leadership configuration", faults.WithReason("invalid_leadership_key"), faults.WithOperation("leadership.Config.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if config.Owner == "" || config.Owner != strings.TrimSpace(config.Owner) || len(config.Owner) > lease.MaximumOwnerLength ||
		config.TTL <= 0 || config.RenewInterval <= 0 || config.RenewInterval >= config.TTL ||
		config.AcquireInterval <= 0 || config.ReleaseTimeout <= 0 {
		return faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, "invalid leadership configuration", faults.WithReason("invalid_leadership_config"), faults.WithOperation("leadership.Config.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

type EventKind string

const (
	EventAcquired  EventKind = "acquired"
	EventRenewed   EventKind = "renewed"
	EventLost      EventKind = "lost"
	EventReleased  EventKind = "released"
	EventContended EventKind = "contended"
)

type Event struct {
	Kind    EventKind
	Session Session
	At      time.Time
	Err     error
}
type Observer interface{ ObserveLeadership(Event) }
type ObserverFunc func(Event)

func (function ObserverFunc) ObserveLeadership(event Event) {
	if function != nil {
		function(event)
	}
}

type Option func(*Elector) error

func WithClock(value mcclock.Clock) Option {
	return func(elector *Elector) error {
		if nilValue(value) {
			return invalid("nil_leadership_clock", "leadership clock must not be nil", "leadership.WithClock")
		}
		elector.clock = value
		return nil
	}
}
func WithRetry(value *retry.Executor) Option {
	return func(elector *Elector) error {
		if value == nil {
			return invalid("nil_leadership_retry", "leadership retry executor must not be nil", "leadership.WithRetry")
		}
		elector.retry = value
		return nil
	}
}
func WithObserver(value Observer) Option {
	return func(elector *Elector) error { elector.observer = value; return nil }
}

type Elector struct {
	store    lease.Store
	config   Config
	handler  Handler
	clock    mcclock.Clock
	retry    *retry.Executor
	observer Observer
	running  atomic.Bool
	leader   atomic.Bool
	mu       sync.RWMutex
	current  lease.Lease
	lastErr  error
}

func New(store lease.Store, config Config, handler Handler, options ...Option) (*Elector, error) {
	if nilValue(store) {
		return nil, invalid("nil_leadership_store", "leadership lease store is required", "leadership.New")
	}
	config = config.normalized()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	policy, err := retry.NewPolicy(retry.WithMaxAttempts(3), retry.WithMaxElapsed(config.RenewInterval))
	if err != nil {
		return nil, err
	}
	executor, err := retry.NewExecutor(policy)
	if err != nil {
		return nil, err
	}
	elector := &Elector{store: store, config: config, handler: handler, clock: mcclock.RealClock{}, retry: executor}
	for _, option := range options {
		if option != nil {
			if err := option(elector); err != nil {
				return nil, err
			}
		}
	}
	return elector, nil
}

func (elector *Elector) Current() (Session, bool) {
	if elector == nil || !elector.leader.Load() {
		return Session{}, false
	}
	elector.mu.RLock()
	value := elector.current
	elector.mu.RUnlock()
	if value.Validate() != nil {
		return Session{}, false
	}
	return Session{Lease: value}, true
}
func (elector *Elector) IsLeader() bool { return elector != nil && elector.leader.Load() }
func (elector *Elector) LastError() error {
	if elector == nil {
		return ErrNotLeader
	}
	elector.mu.RLock()
	defer elector.mu.RUnlock()
	return elector.lastErr
}

func (elector *Elector) Component(name string) servicekit.Component {
	return servicekit.Component{
		Name: name,
		Run:  elector.Run,
		Liveness: func(context.Context) error {
			if elector == nil {
				return invalid("nil_leadership_elector", "leadership elector is not configured", "leadership.Elector.Liveness")
			}
			err := elector.LastError()
			if err != nil && !faults.IsRetryable(err) && !errors.Is(err, lease.ErrHeld) {
				return err
			}
			return nil
		},
		Readiness: func(context.Context) error {
			if elector == nil {
				return ErrNotLeader
			}
			if !elector.config.RequireLeaderReadiness || elector.IsLeader() {
				return nil
			}
			return faults.Wrap(ErrNotLeader, faults.CodeUnavailable, "process does not currently hold leadership", faults.WithReason("leadership_not_held"), faults.WithOperation("leadership.Elector.Readiness"), faults.WithRetryPolicy(faults.ImmediateRetry(0)))
		},
	}
}

func (elector *Elector) Run(ctx context.Context) error {
	if ctx == nil {
		return invalid("nil_context", "leadership context is required", "leadership.Elector.Run")
	}
	if elector == nil || nilValue(elector.store) || nilValue(elector.clock) || elector.retry == nil {
		return invalid("invalid_leadership_elector", "leadership elector is not configured", "leadership.Elector.Run")
	}
	if !elector.running.CompareAndSwap(false, true) {
		return faults.Wrap(ErrAlreadyRun, faults.CodeFailedPrecondition, "leadership elector has already run", faults.WithReason("leadership_already_run"), faults.WithOperation("leadership.Elector.Run"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	defer elector.running.Store(false)
	for {
		if ctx.Err() != nil {
			return nil
		}
		value, err := elector.acquire(ctx)
		if err != nil {
			elector.setError(err)
			elector.observe(Event{Kind: EventContended, At: elector.clock.Now(), Err: err})
			if !retryableAcquire(err) {
				return err
			}
			if sleepErr := elector.clock.Sleep(ctx, acquireDelay(err, elector.config.AcquireInterval)); sleepErr != nil {
				return nil
			}
			continue
		}
		session := Session{Lease: value}
		elector.setLeadership(value)
		elector.observe(Event{Kind: EventAcquired, Session: session, At: elector.clock.Now()})
		err = elector.hold(ctx, session)
		elector.clearLeadership(err)
		if ctx.Err() != nil {
			elector.release(value)
			return nil
		}
		if err == nil {
			elector.release(value)
			return nil
		}
		if errors.Is(err, ErrLeadershipLost) || errors.Is(err, lease.ErrStale) || errors.Is(err, lease.ErrNotFound) {
			elector.observe(Event{Kind: EventLost, Session: session, At: elector.clock.Now(), Err: err})
			if elector.config.ExitOnLeadershipLoss {
				return err
			}
			if sleepErr := elector.clock.Sleep(ctx, elector.config.AcquireInterval); sleepErr != nil {
				return nil
			}
			continue
		}
		elector.release(value)
		return err
	}
}

// GateComponent transfers ownership of component.Run to a leadership Handler.
// The returned component retains startup, shutdown, and health hooks, but has
// no independent Run loop; the elector is therefore the only path that can
// start the work. This prevents standby replicas from doing singleton work
// while still registering the component at its canonical servicekit stage.
func GateComponent(component servicekit.Component) (Handler, servicekit.Component, error) {
	if strings.TrimSpace(component.Name) == "" || component.Run == nil {
		return nil, servicekit.Component{}, invalid(
			"invalid_leader_managed_component",
			"leader-managed component requires a name and run function",
			"leadership.GateComponent",
		)
	}
	run := component.Run
	component.Run = nil
	handler := func(ctx context.Context, _ Session) error {
		return run(ctx)
	}
	return handler, component, nil
}

func (elector *Elector) acquire(ctx context.Context) (lease.Lease, error) {
	var value lease.Lease
	_, err := elector.retry.Do(ctx, "leadership.acquire", func(attemptCtx context.Context, _ retry.Attempt) error {
		acquired, acquireErr := elector.store.Acquire(attemptCtx, lease.AcquireRequest{Key: elector.config.Key, Owner: elector.config.Owner, TTL: elector.config.TTL})
		if acquireErr == nil {
			value = acquired
		}
		return acquireErr
	})
	return value, err
}

func (elector *Elector) hold(parent context.Context, session Session) error {
	leaderCtx, cancel := context.WithCancelCause(parent)
	defer cancel(ErrLeadershipLost)
	handlerDone := make(chan error, 1)
	if elector.handler != nil {
		go func() { handlerDone <- elector.handler(leaderCtx, session) }()
	}
	for {
		waitCtx, waitCancel := context.WithCancel(leaderCtx)
		sleepDone := make(chan error, 1)
		go func() { sleepDone <- elector.clock.Sleep(waitCtx, elector.config.RenewInterval) }()
		select {
		case <-parent.Done():
			waitCancel()
			<-sleepDone
			cancel(context.Cause(parent))
			return nil
		case err := <-handlerDone:
			waitCancel()
			<-sleepDone
			return err
		case <-sleepDone:
			waitCancel()
		}
		var renewed lease.Lease
		_, err := elector.retry.Do(leaderCtx, "leadership.renew", func(attemptCtx context.Context, _ retry.Attempt) error {
			value, renewErr := elector.store.Renew(attemptCtx, session.Lease, elector.config.TTL)
			if renewErr == nil {
				renewed = value
			}
			return renewErr
		})
		if err != nil {
			cancel(errors.Join(ErrLeadershipLost, err))
			if elector.handler != nil {
				<-handlerDone
			}
			return faults.Wrap(errors.Join(ErrLeadershipLost, err), faults.CodeConflict, "leadership lease could not be renewed", faults.WithReason("leadership_lost"), faults.WithOperation("leadership.Elector.hold"), faults.WithRetryPolicy(faults.ImmediateRetry(0)))
		}
		session.Lease = renewed
		elector.setLeadership(renewed)
		elector.observe(Event{Kind: EventRenewed, Session: session, At: elector.clock.Now()})
	}
}

func (elector *Elector) release(value lease.Lease) {
	ctx, cancel := context.WithTimeout(context.Background(), elector.config.ReleaseTimeout)
	defer cancel()
	err := elector.store.Release(ctx, value)
	if err != nil && !errors.Is(err, lease.ErrStale) && !errors.Is(err, lease.ErrNotFound) {
		elector.setError(err)
		return
	}
	elector.observe(Event{Kind: EventReleased, Session: Session{Lease: value}, At: elector.clock.Now(), Err: err})
}
func (elector *Elector) setLeadership(value lease.Lease) {
	elector.mu.Lock()
	elector.current = value
	elector.lastErr = nil
	elector.mu.Unlock()
	elector.leader.Store(true)
}
func (elector *Elector) clearLeadership(err error) {
	elector.leader.Store(false)
	elector.mu.Lock()
	elector.current = lease.Lease{}
	elector.lastErr = err
	elector.mu.Unlock()
}
func (elector *Elector) setError(err error) {
	elector.mu.Lock()
	elector.lastErr = err
	elector.mu.Unlock()
}
func (elector *Elector) observe(event Event) {
	if elector == nil || nilValue(elector.observer) {
		return
	}
	defer func() { _ = recover() }()
	elector.observer.ObserveLeadership(event)
}

func retryableAcquire(err error) bool {
	return errors.Is(err, lease.ErrHeld) || faults.IsRetryable(err)
}
func acquireDelay(err error, fallback time.Duration) time.Duration {
	policy := faults.RetryPolicyOf(err).Normalized()
	if policy.After > fallback {
		return policy.After
	}
	return fallback
}
func invalid(reason, message, operation string) error {
	return faults.Wrap(ErrInvalidConfig, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
}
func nilValue(value any) bool {
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

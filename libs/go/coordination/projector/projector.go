// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package projector

import (
	"context"
	"errors"
	mcclock "mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/coordination/cursor"
	"mindclade.internal/libs/go/coordination/inbox"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/idempotency"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
	"mindclade.internal/libs/go/servicekit"
	"reflect"
	"sync/atomic"
	"time"
)

const (
	DefaultPollInterval = time.Second
	DefaultBatchSize    = 100
)

type Event struct {
	Identity    idempotency.Identity
	Fingerprint identifiers.Digest
	Sequence    uint64
	Opaque      []byte
	RequestID   requestmeta.RequestID
	Payload     []byte
}

func (event Event) Validate() error {
	if err := event.Identity.Validate(); err != nil {
		return err
	}
	if !event.Fingerprint.Valid() || len(event.Opaque) > cursor.MaximumOpaqueBytes {
		return invalid("invalid_projection_event", "invalid projection event", "projector.Event.Validate")
	}
	return nil
}

type Source interface {
	Fetch(context.Context, *cursor.Cursor, int) ([]Event, error)
}
type Handler interface {
	Apply(context.Context, Event) (idempotency.Result, error)
}
type HandlerFunc func(context.Context, Event) (idempotency.Result, error)

func (function HandlerFunc) Apply(ctx context.Context, event Event) (idempotency.Result, error) {
	return function(ctx, event)
}

type FenceProvider interface{ Fence() (uint64, bool) }
type FenceProviderFunc func() (uint64, bool)

func (function FenceProviderFunc) Fence() (uint64, bool) { return function() }

type Config struct {
	Cursor        cursor.Key
	PollInterval  time.Duration
	BatchSize     int
	MessageTTL    time.Duration
	LeaseDuration time.Duration
}

func (config Config) normalized() Config {
	if config.PollInterval == 0 {
		config.PollInterval = DefaultPollInterval
	}
	if config.BatchSize == 0 {
		config.BatchSize = DefaultBatchSize
	}
	if config.MessageTTL == 0 {
		config.MessageTTL = idempotency.DefaultRecordTTL
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = idempotency.DefaultLeaseDuration
	}
	return config
}
func (config Config) Validate() error {
	config = config.normalized()
	if err := config.Cursor.Validate(); err != nil || config.PollInterval <= 0 || config.BatchSize <= 0 || config.BatchSize > 10000 || config.MessageTTL <= 0 || config.LeaseDuration <= 0 {
		return invalid("invalid_projector_config", "invalid projector configuration", "projector.Config.Validate")
	}
	return nil
}

type Option func(*Processor) error

func WithClock(value mcclock.Clock) Option {
	return func(processor *Processor) error {
		if nilValue(value) {
			return invalid("nil_projector_clock", "projector clock is required", "projector.WithClock")
		}
		processor.clock = value
		return nil
	}
}

type Processor struct {
	source  Source
	handler Handler
	inbox   *inbox.Processor
	cursors cursor.Store
	fence   FenceProvider
	config  Config
	clock   mcclock.Clock
	running atomic.Bool
	ready   atomic.Bool
}

func New(source Source, handler Handler, inboxProcessor *inbox.Processor, cursors cursor.Store, fence FenceProvider, config Config, options ...Option) (*Processor, error) {
	if nilValue(source) || nilValue(handler) || inboxProcessor == nil || nilValue(cursors) || nilValue(fence) {
		return nil, invalid("missing_projector_dependencies", "projector dependencies are required", "projector.New")
	}
	config = config.normalized()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	processor := &Processor{source: source, handler: handler, inbox: inboxProcessor, cursors: cursors, fence: fence, config: config, clock: mcclock.RealClock{}}
	for _, option := range options {
		if option != nil {
			if err := option(processor); err != nil {
				return nil, err
			}
		}
	}
	return processor, nil
}
func (processor *Processor) Run(ctx context.Context) error {
	if ctx == nil {
		return invalid("nil_context", "projector context is required", "projector.Processor.Run")
	}
	if !processor.running.CompareAndSwap(false, true) {
		return faults.Wrap(ErrStopped, faults.CodeFailedPrecondition, "projector already ran", faults.WithReason("projector_already_run"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	processor.ready.Store(true)
	defer processor.ready.Store(false)
	var current *cursor.Cursor
	if loaded, err := processor.cursors.Load(ctx, processor.config.Cursor); err == nil {
		current = &loaded
	} else if !errors.Is(err, cursor.ErrNotFound) {
		return err
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		fence, ok := processor.fence.Fence()
		if !ok || fence == 0 {
			processor.ready.Store(false)
			if err := processor.clock.Sleep(ctx, processor.config.PollInterval); err != nil {
				return nil
			}
			continue
		}
		events, err := processor.source.Fetch(ctx, current, processor.config.BatchSize)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			processor.ready.Store(true)
			if err := processor.clock.Sleep(ctx, processor.config.PollInterval); err != nil {
				return nil
			}
			continue
		}
		for _, event := range events {
			if err := event.Validate(); err != nil {
				return err
			}
			expected := uint64(0)
			if current != nil {
				expected = current.Version
			}
			var advanced cursor.Cursor
			outcome, err := processor.inbox.Process(ctx, inbox.Message{Identity: event.Identity, Fingerprint: event.Fingerprint, RequestID: event.RequestID, TTL: processor.config.MessageTTL, LeaseDuration: processor.config.LeaseDuration}, func(txctx context.Context) (idempotency.Result, error) {
				result, applyErr := processor.handler.Apply(txctx, event)
				if applyErr != nil {
					return idempotency.Result{}, applyErr
				}
				next, advanceErr := processor.cursors.Advance(txctx, cursor.AdvanceRequest{Key: processor.config.Cursor, ExpectedVersion: expected, Sequence: event.Sequence, Opaque: event.Opaque, Fence: fence, UpdatedAt: processor.clock.Now().UTC()})
				if advanceErr != nil {
					return idempotency.Result{}, advanceErr
				}
				advanced = next
				return result, nil
			})
			if err != nil {
				return err
			}
			if outcome.Duplicate {
				loaded, loadErr := processor.cursors.Load(ctx, processor.config.Cursor)
				if loadErr != nil {
					return loadErr
				}
				advanced = loaded
			}
			current = &advanced
		}
	}
}
func (processor *Processor) Component(name string) servicekit.Component {
	return servicekit.Component{Name: name, Run: processor.Run, Liveness: func(context.Context) error {
		if processor == nil {
			return ErrStopped
		}
		return nil
	}, Readiness: func(context.Context) error {
		if processor != nil && processor.ready.Load() {
			return nil
		}
		return faults.Wrap(ErrStopped, faults.CodeUnavailable, "projector is not ready", faults.WithReason("projector_not_ready"), faults.WithRetryPolicy(faults.ImmediateRetry(0)))
	}}
}
func invalid(reason, message, op string) error {
	return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation(op), faults.WithRetryPolicy(faults.NoRetry()))
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

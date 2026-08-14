// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package pubsub

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/messaging"
)

// Subscription adapts one provider receiver with a strict handler-concurrency
// bound and deterministic settlement behavior.
type Subscription struct {
	config   Config
	receiver Receiver
	closed   atomic.Bool
	once     sync.Once
}

func NewSubscription(config Config, receiver Receiver) (*Subscription, error) {
	normalized, err := config.normalized()
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, faults.New(faults.CodeInvalidArgument, "Pub/Sub receiver is required", faults.WithReason("nil_pubsub_receiver"), faults.WithOperation("messaging.pubsub.NewSubscription"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return &Subscription{config: normalized, receiver: receiver}, nil
}

func (subscription *Subscription) Receive(ctx context.Context, handler messaging.Handler) error {
	if ctx == nil || subscription == nil || handler == nil {
		return faults.Wrap(messaging.ErrInvalidSubscription, faults.CodeInvalidArgument, "invalid Pub/Sub receive request", faults.WithReason("invalid_pubsub_receive"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if subscription.closed.Load() {
		return messaging.ErrClosed
	}
	semaphore := make(chan struct{}, subscription.config.MaxConcurrentHandlers)
	return subscription.receiver.Receive(ctx, func(providerContext context.Context, providerDelivery ProviderDelivery) {
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			providerDelivery.Nack()
			return
		case <-providerContext.Done():
			providerDelivery.Nack()
			return
		}
		defer func() { <-semaphore }()
		delivery, err := newDelivery(subscription.config, providerDelivery)
		if err != nil {
			providerDelivery.Nack()
			return
		}
		handlerContext, cancel := context.WithTimeout(providerContext, subscription.config.AckDeadline)
		err = safeHandle(handlerContext, handler, delivery)
		cancel()
		if delivery.Settled() {
			return
		}
		if err == nil {
			_ = delivery.Ack(providerContext)
		} else {
			_ = delivery.Nack(providerContext)
		}
	})
}

func (subscription *Subscription) Close(context.Context) error {
	if subscription == nil {
		return nil
	}
	subscription.once.Do(func() {
		subscription.closed.Store(true)
		subscription.receiver.Stop()
	})
	return nil
}

func safeHandle(ctx context.Context, handler messaging.Handler, delivery messaging.Delivery) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("Pub/Sub handler panic: %v", recovered)
		}
	}()
	return handler(ctx, delivery)
}

type delivery struct {
	provider ProviderDelivery
	message  messaging.Message
	attempt  int
	deadline atomic.Int64
	settled  atomic.Bool
}

func newDelivery(config Config, provider ProviderDelivery) (*delivery, error) {
	message, err := decode(config, provider)
	if err != nil {
		return nil, err
	}
	attempt := provider.DeliveryAttempt()
	if attempt < 1 {
		attempt = 1
	}
	value := &delivery{provider: provider, message: message, attempt: attempt}
	value.deadline.Store(time.Now().Add(config.AckDeadline).UnixNano())
	return value, nil
}

func (delivery *delivery) Message() messaging.Message { return delivery.message }
func (delivery *delivery) Attempt() int               { return delivery.attempt }
func (delivery *delivery) Deadline() time.Time        { return time.Unix(0, delivery.deadline.Load()).UTC() }
func (delivery *delivery) Settled() bool              { return delivery.settled.Load() }

func (delivery *delivery) Ack(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if !delivery.settled.CompareAndSwap(false, true) {
		return messaging.ErrAlreadySettled
	}
	delivery.provider.Ack()
	return nil
}

func (delivery *delivery) Nack(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if !delivery.settled.CompareAndSwap(false, true) {
		return messaging.ErrAlreadySettled
	}
	delivery.provider.Nack()
	return nil
}

func (delivery *delivery) Extend(ctx context.Context, duration time.Duration) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if delivery.Settled() {
		return messaging.ErrAlreadySettled
	}
	if duration <= 0 {
		return faults.Wrap(messaging.ErrInvalidSubscription, faults.CodeInvalidArgument, "invalid Pub/Sub acknowledgement extension", faults.WithReason("invalid_pubsub_ack_extension"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := delivery.provider.Extend(duration); err != nil {
		return faults.Wrap(err, faults.CodeUnavailable, "Pub/Sub acknowledgement extension failed", faults.WithReason("pubsub_ack_extension_failed"), faults.WithRetryPolicy(faults.BackoffRetry(0)))
	}
	delivery.deadline.Store(time.Now().Add(duration).UnixNano())
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return context.Canceled
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

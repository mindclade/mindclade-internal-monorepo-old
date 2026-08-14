// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/messaging"
	"mindclade.internal/libs/go/requestmeta"
)

const (
	DefaultCapacity       = 1024
	DefaultMaxAttempts    = 10
	DefaultAckDeadline    = 30 * time.Second
	DefaultHandlerWorkers = 1
)

type Config struct {
	Capacity       int
	MaxAttempts    int
	AckDeadline    time.Duration
	HandlerWorkers int
	Clock          clock.Clock
}

func (config Config) normalized() (Config, error) {
	if config.Capacity == 0 {
		config.Capacity = DefaultCapacity
	}
	if config.MaxAttempts == 0 {
		config.MaxAttempts = DefaultMaxAttempts
	}
	if config.AckDeadline == 0 {
		config.AckDeadline = DefaultAckDeadline
	}
	if config.HandlerWorkers == 0 {
		config.HandlerWorkers = DefaultHandlerWorkers
	}
	if config.Clock == nil {
		config.Clock = clock.RealClock{}
	}
	if config.Capacity < 1 || config.MaxAttempts < 1 || config.AckDeadline < 0 || config.HandlerWorkers < 1 {
		return Config{}, faults.Wrap(messaging.ErrInvalidSubscription, faults.CodeInvalidArgument, "invalid memory broker configuration", faults.WithReason("invalid_memory_broker_config"), faults.WithOperation("messaging.memory.NewBroker"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return config, nil
}

type Broker struct {
	config           Config
	mu               sync.RWMutex
	closed           bool
	subscriptions    map[string][]*subscription
	providerSequence atomic.Uint64
}

func NewBroker(config Config) (*Broker, error) {
	normalized, err := config.normalized()
	if err != nil {
		return nil, err
	}
	return &Broker{config: normalized, subscriptions: make(map[string][]*subscription)}, nil
}

func (broker *Broker) Subscribe(topic string) (messaging.Subscription, error) {
	if broker == nil {
		return nil, messaging.ErrInvalidSubscription
	}
	probe, err := messageForTopic(topic, broker.config.Clock.Now())
	if err != nil {
		return nil, err
	}
	topic = probe.Topic()
	sub := &subscription{
		broker: broker,
		topic:  topic,
		queue:  make(chan queuedDelivery, broker.config.Capacity),
		closed: make(chan struct{}),
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if broker.closed {
		return nil, messaging.ErrClosed
	}
	broker.subscriptions[topic] = append(broker.subscriptions[topic], sub)
	return sub, nil
}

func messageForTopic(topic string, now time.Time) (messaging.Message, error) {
	// Use NewMessage validation without exporting token internals.
	id, err := identifiers.NewIDAt(messaging.MessageIDKind, now)
	if err != nil {
		return messaging.Message{}, err
	}
	return messaging.NewMessage(id, topic, "", "application/octet-stream", []byte{0}, nil, requestmeta.Metadata{}, now)
}

func (broker *Broker) Publish(ctx context.Context, message messaging.Message) (messaging.Publication, error) {
	if ctx == nil {
		return messaging.Publication{}, context.Canceled
	}
	if broker == nil {
		return messaging.Publication{}, messaging.ErrClosed
	}
	if err := message.Validate(); err != nil {
		return messaging.Publication{}, err
	}
	select {
	case <-ctx.Done():
		return messaging.Publication{}, ctx.Err()
	default:
	}

	broker.mu.RLock()
	if broker.closed {
		broker.mu.RUnlock()
		return messaging.Publication{}, messaging.ErrClosed
	}
	subscribers := append([]*subscription(nil), broker.subscriptions[message.Topic()]...)
	broker.mu.RUnlock()

	for _, sub := range subscribers {
		delivery := queuedDelivery{message: message, attempt: 1}
		select {
		case <-ctx.Done():
			return messaging.Publication{}, ctx.Err()
		case <-sub.closed:
			continue
		case sub.queue <- delivery:
		default:
			return messaging.Publication{}, faults.Wrap(messaging.ErrCapacityExceeded, faults.CodeResourceExhausted, "messaging subscription is full", faults.WithReason("subscription_capacity_exceeded"), faults.WithOperation("messaging.memory.Broker.Publish"), faults.WithField("topic", message.Topic()), faults.WithRetryPolicy(faults.BackoffRetry(0)))
		}
	}
	sequence := broker.providerSequence.Add(1)
	return messaging.Publication{MessageID: message.ID(), ProviderID: fmt.Sprintf("memory-%d", sequence), PublishedAt: broker.config.Clock.Now().UTC()}, nil
}

func (broker *Broker) Close(ctx context.Context) error {
	if broker == nil {
		return nil
	}
	broker.mu.Lock()
	if broker.closed {
		broker.mu.Unlock()
		return nil
	}
	broker.closed = true
	var subscriptions []*subscription
	for _, values := range broker.subscriptions {
		subscriptions = append(subscriptions, values...)
	}
	broker.subscriptions = nil
	broker.mu.Unlock()
	for _, sub := range subscriptions {
		_ = sub.Close(ctx)
	}
	return nil
}

type queuedDelivery struct {
	message messaging.Message
	attempt int
}

type subscription struct {
	broker *Broker
	topic  string
	queue  chan queuedDelivery
	once   sync.Once
	closed chan struct{}
}

func (sub *subscription) Receive(ctx context.Context, handler messaging.Handler) error {
	if ctx == nil {
		return context.Canceled
	}
	if handler == nil {
		return messaging.ErrInvalidSubscription
	}
	workers := sub.broker.config.HandlerWorkers
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-workerCtx.Done():
					return
				case <-sub.closed:
					return
				case queued := <-sub.queue:
					delivery := newDelivery(sub, queued)
					handlerCtx, handlerCancel := context.WithTimeout(workerCtx, sub.broker.config.AckDeadline)
					err := safeHandle(handlerCtx, handler, delivery)
					handlerCancel()
					if delivery.Settled() {
						continue
					}
					var settlementErr error
					if err == nil {
						settlementErr = delivery.Ack(workerCtx)
					} else {
						// Handler failures are delivery-local. A successful Nack hands
						// responsibility back to the broker for redelivery and must not
						// terminate the subscription receive loop.
						settlementErr = delivery.Nack(workerCtx)
					}
					if settlementErr != nil && !errors.Is(settlementErr, messaging.ErrAlreadySettled) {
						select {
						case results <- settlementErr:
						default:
						}
					}
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		cancel()
		<-done
		return nil
	case <-sub.closed:
		cancel()
		<-done
		return nil
	case err := <-results:
		cancel()
		<-done
		return err
	}
}

func safeHandle(ctx context.Context, handler messaging.Handler, delivery messaging.Delivery) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("messaging handler panic: %v", recovered)
		}
	}()
	return handler(ctx, delivery)
}

func (sub *subscription) Close(context.Context) error {
	if sub == nil {
		return nil
	}
	sub.once.Do(func() { close(sub.closed) })
	return nil
}

type delivery struct {
	sub        *subscription
	message    messaging.Message
	attempt    int
	deadlineMu sync.RWMutex
	deadline   time.Time
	settled    atomic.Bool
}

func newDelivery(sub *subscription, queued queuedDelivery) *delivery {
	return &delivery{sub: sub, message: queued.message, attempt: queued.attempt, deadline: sub.broker.config.Clock.Now().Add(sub.broker.config.AckDeadline)}
}
func (value *delivery) Message() messaging.Message { return value.message }
func (value *delivery) Attempt() int               { return value.attempt }
func (value *delivery) Deadline() time.Time {
	value.deadlineMu.RLock()
	defer value.deadlineMu.RUnlock()
	return value.deadline
}
func (value *delivery) Settled() bool { return value.settled.Load() }
func (value *delivery) Ack(context.Context) error {
	if !value.settled.CompareAndSwap(false, true) {
		return messaging.ErrAlreadySettled
	}
	return nil
}
func (value *delivery) Nack(ctx context.Context) error {
	if !value.settled.CompareAndSwap(false, true) {
		return messaging.ErrAlreadySettled
	}
	if value.attempt >= value.sub.broker.config.MaxAttempts {
		return nil
	}
	next := queuedDelivery{message: value.message, attempt: value.attempt + 1}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-value.sub.closed:
		return messaging.ErrClosed
	case value.sub.queue <- next:
		return nil
	default:
		return messaging.ErrCapacityExceeded
	}
}
func (value *delivery) Extend(ctx context.Context, duration time.Duration) error {
	if value.Settled() {
		return messaging.ErrAlreadySettled
	}
	if duration <= 0 {
		return messaging.ErrInvalidSubscription
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	value.deadlineMu.Lock()
	value.deadline = value.sub.broker.config.Clock.Now().Add(duration)
	value.deadlineMu.Unlock()
	return nil
}

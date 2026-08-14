// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package outbox

import (
	"time"

	mcclock "mindclade.internal/libs/go/clock"
	coordination "mindclade.internal/libs/go/coordination/outbox"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
	"mindclade.internal/libs/go/retry"
)

const (
	DefaultPollInterval  = coordination.DefaultPollInterval
	DefaultClaimDuration = coordination.DefaultClaimDuration
	DefaultBatchSize     = coordination.DefaultBatchSize
	DefaultMaxDeliveries = coordination.DefaultMaxDeliveries
)

type Publisher = coordination.Publisher
type PublisherFunc = coordination.PublisherFunc
type Dispatcher = coordination.Dispatcher
type DispatcherConfig = coordination.DispatcherConfig
type DispatcherOption = coordination.DispatcherOption
type DispatchEvent = coordination.DispatchEvent
type DispatchEventKind = coordination.DispatchEventKind
type DispatchObserver = coordination.DispatchObserver
type DispatchObserverFunc = coordination.DispatchObserverFunc
type Factory = coordination.Factory
type FactoryOption = coordination.FactoryOption

const (
	DispatchClaimed     DispatchEventKind = coordination.DispatchClaimed
	DispatchPublished   DispatchEventKind = coordination.DispatchPublished
	DispatchRescheduled DispatchEventKind = coordination.DispatchRescheduled
	DispatchDeadLetter  DispatchEventKind = coordination.DispatchDeadLetter
	DispatchFailed      DispatchEventKind = coordination.DispatchFailed
)

func NewDispatcher(repository Repository, publisher Publisher, config DispatcherConfig, options ...DispatcherOption) (*Dispatcher, error) {
	return coordination.NewDispatcher(repository, publisher, config, options...)
}

func WithDispatcherClock(value mcclock.Clock) DispatcherOption {
	return coordination.WithDispatcherClock(value)
}

func WithDispatchObserver(value DispatchObserver) DispatcherOption {
	return coordination.WithDispatchObserver(value)
}

func WithPublishRetry(value *retry.Executor) DispatcherOption {
	return coordination.WithPublishRetry(value)
}

func NewFactory(options ...FactoryOption) (*Factory, error) {
	return coordination.NewFactory(options...)
}

func WithFactoryClock(value mcclock.Clock) FactoryOption {
	return coordination.WithFactoryClock(value)
}

func WithFactoryGenerator(value *identifiers.Generator) FactoryOption {
	return coordination.WithFactoryGenerator(value)
}

// CreateEnvelope invokes the canonical factory while retaining storage-facing terminology.
func CreateEnvelope(factory *Factory, topic, partitionKey, contentType string, payload []byte, headers map[string]string, request requestmeta.Metadata, availableAt time.Time) (Envelope, error) {
	if factory == nil {
		return Envelope{}, ErrInvalidRequest
	}
	return factory.Create(topic, partitionKey, contentType, payload, headers, request, availableAt)
}

// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package pubsub

import (
	"context"
	"time"
)

// PublishMessage is the provider-neutral subset required from a Pub/Sub SDK.
type PublishMessage struct {
	Data        []byte
	Attributes  map[string]string
	OrderingKey string
}

// PublishResult waits for provider acceptance and returns its message ID.
type PublishResult interface {
	Get(context.Context) (string, error)
}

// Topic is one provider topic handle. Concrete cloud SDK adapters normally
// implement this as a thin wrapper around their native Topic type.
type Topic interface {
	Publish(context.Context, PublishMessage) PublishResult
	Stop()
}

// TopicResolver resolves the immutable logical message topic to a provider
// topic handle. Resolver implementations may cache handles.
type TopicResolver interface {
	Topic(context.Context, string) (Topic, error)
}

// ProviderDelivery is the subset required from a provider message delivery.
type ProviderDelivery interface {
	ProviderID() string
	Data() []byte
	Attributes() map[string]string
	OrderingKey() string
	PublishTime() time.Time
	DeliveryAttempt() int
	Ack()
	Nack()
	Extend(time.Duration) error
}

// Receiver owns the provider receive loop. Its callback may be invoked
// concurrently; Subscription applies an additional strict concurrency bound.
type Receiver interface {
	Receive(context.Context, func(context.Context, ProviderDelivery)) error
	Stop()
}

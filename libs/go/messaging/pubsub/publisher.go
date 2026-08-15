// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package pubsub

import (
	"context"
	"strings"
	"sync"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/messaging"
)

// Publisher maps immutable Mindclade messages onto provider messages.
type Publisher struct {
	resolver TopicResolver
	mu       sync.Mutex
	topics   map[string]Topic
	closed   bool
}

func NewPublisher(resolver TopicResolver) (*Publisher, error) {
	if resolver == nil {
		return nil, faults.New(faults.CodeInvalidArgument, "Pub/Sub topic resolver is required", faults.WithReason("nil_pubsub_topic_resolver"), faults.WithOperation("messaging.pubsub.NewPublisher"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return &Publisher{resolver: resolver, topics: make(map[string]Topic)}, nil
}

func (publisher *Publisher) Publish(ctx context.Context, message messaging.Message) (messaging.Publication, error) {
	if ctx == nil || publisher == nil {
		return messaging.Publication{}, faults.Wrap(messaging.ErrPublishFailed, faults.CodeInvalidArgument, "invalid Pub/Sub publish request", faults.WithReason("invalid_pubsub_publish"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	providerMessage, err := encode(message)
	if err != nil {
		return messaging.Publication{}, err
	}
	topic, err := publisher.topic(ctx, message.Topic())
	if err != nil {
		return messaging.Publication{}, err
	}
	result := topic.Publish(ctx, providerMessage)
	if result == nil {
		return messaging.Publication{}, faults.Wrap(messaging.ErrPublishFailed, faults.CodeUnavailable, "Pub/Sub provider returned no publish result", faults.WithReason("nil_pubsub_publish_result"), faults.WithOperation("messaging.pubsub.Publisher.Publish"), faults.WithRetryPolicy(faults.BackoffRetry(0)))
	}
	providerID, err := result.Get(ctx)
	if err != nil {
		return messaging.Publication{}, faults.Wrap(err, faults.CodeUnavailable, "Pub/Sub publish failed", faults.WithReason("pubsub_publish_failed"), faults.WithOperation("messaging.pubsub.Publisher.Publish"), faults.WithRetryPolicy(faults.BackoffRetry(0)))
	}
	if strings.TrimSpace(providerID) == "" {
		return messaging.Publication{}, faults.Wrap(messaging.ErrPublishFailed, faults.CodeDataLoss, "Pub/Sub provider returned an empty message ID", faults.WithReason("empty_pubsub_provider_id"), faults.WithOperation("messaging.pubsub.Publisher.Publish"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return messaging.Publication{MessageID: message.ID(), ProviderID: providerID, PublishedAt: time.Now().UTC()}, nil
}

func (publisher *Publisher) topic(ctx context.Context, name string) (Topic, error) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if publisher.closed {
		return nil, messaging.ErrClosed
	}
	if topic := publisher.topics[name]; topic != nil {
		return topic, nil
	}
	topic, err := publisher.resolver.Topic(ctx, name)
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeUnavailable, "Pub/Sub topic resolution failed", faults.WithReason("pubsub_topic_resolution_failed"), faults.WithOperation("messaging.pubsub.Publisher.Topic"), faults.WithField("topic", name), faults.WithRetryPolicy(faults.BackoffRetry(0)))
	}
	if topic == nil {
		return nil, faults.Wrap(messaging.ErrPublishFailed, faults.CodeUnavailable, "Pub/Sub topic resolution returned nil", faults.WithReason("nil_pubsub_topic"), faults.WithOperation("messaging.pubsub.Publisher.Topic"), faults.WithField("topic", name), faults.WithRetryPolicy(faults.BackoffRetry(0)))
	}
	publisher.topics[name] = topic
	return topic, nil
}

func (publisher *Publisher) Close(context.Context) error {
	if publisher == nil {
		return nil
	}
	publisher.mu.Lock()
	if publisher.closed {
		publisher.mu.Unlock()
		return nil
	}
	publisher.closed = true
	topics := publisher.topics
	publisher.topics = nil
	publisher.mu.Unlock()
	for _, topic := range topics {
		if topic != nil {
			topic.Stop()
		}
	}
	return nil
}

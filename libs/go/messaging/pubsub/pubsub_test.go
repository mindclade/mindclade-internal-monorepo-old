// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package pubsub

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/messaging"
	"mindclade.internal/libs/go/requestmeta"
)

type result string

func (value result) Get(context.Context) (string, error) { return string(value), nil }

type fakeTopic struct {
	message PublishMessage
	stopped atomic.Bool
}

func (topic *fakeTopic) Publish(_ context.Context, message PublishMessage) PublishResult {
	topic.message = message
	return result("provider-1")
}
func (topic *fakeTopic) Stop() { topic.stopped.Store(true) }

type resolver struct{ topic *fakeTopic }

func (value resolver) Topic(context.Context, string) (Topic, error) { return value.topic, nil }

type fakeProviderDelivery struct {
	message  PublishMessage
	acked    atomic.Bool
	nacked   atomic.Bool
	extended atomic.Int64
}

func (value *fakeProviderDelivery) ProviderID() string { return "provider-1" }
func (value *fakeProviderDelivery) Data() []byte       { return append([]byte(nil), value.message.Data...) }
func (value *fakeProviderDelivery) Attributes() map[string]string {
	return clone(value.message.Attributes)
}
func (value *fakeProviderDelivery) OrderingKey() string    { return value.message.OrderingKey }
func (value *fakeProviderDelivery) PublishTime() time.Time { return time.Now() }
func (value *fakeProviderDelivery) DeliveryAttempt() int   { return 2 }
func (value *fakeProviderDelivery) Ack()                   { value.acked.Store(true) }
func (value *fakeProviderDelivery) Nack()                  { value.nacked.Store(true) }
func (value *fakeProviderDelivery) Extend(duration time.Duration) error {
	value.extended.Store(int64(duration))
	return nil
}

type fakeReceiver struct {
	delivery ProviderDelivery
	stopped  atomic.Bool
}

func (value *fakeReceiver) Receive(ctx context.Context, handler func(context.Context, ProviderDelivery)) error {
	handler(ctx, value.delivery)
	return nil
}
func (value *fakeReceiver) Stop() { value.stopped.Store(true) }

func TestPublishAndReceiveRoundTrip(t *testing.T) {
	now := time.Now().UTC()
	id, err := identifiers.NewIDAt(messaging.MessageIDKind, now)
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := requestmeta.NewRequestIDAt(now)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := requestmeta.New(requestID)
	if err != nil {
		t.Fatal(err)
	}
	message, err := messaging.NewMessage(id, "control.events", "run-1", "application/protobuf", []byte("payload"), map[string]string{"schema": "v1"}, metadata, now)
	if err != nil {
		t.Fatal(err)
	}

	topic := &fakeTopic{}
	publisher, err := NewPublisher(resolver{topic: topic})
	if err != nil {
		t.Fatal(err)
	}
	publication, err := publisher.Publish(context.Background(), message)
	if err != nil {
		t.Fatal(err)
	}
	if publication.ProviderID != "provider-1" || publication.MessageID != message.ID() {
		t.Fatalf("publication=%+v", publication)
	}

	providerDelivery := &fakeProviderDelivery{message: topic.message}
	receiver := &fakeReceiver{delivery: providerDelivery}
	subscription, err := NewSubscription(Config{Topic: message.Topic(), MaxConcurrentHandlers: 1, AckDeadline: time.Second}, receiver)
	if err != nil {
		t.Fatal(err)
	}
	var got messaging.Message
	if err := subscription.Receive(context.Background(), func(_ context.Context, delivery messaging.Delivery) error {
		got = delivery.Message()
		if delivery.Attempt() != 2 {
			t.Fatalf("attempt=%d", delivery.Attempt())
		}
		return delivery.Extend(context.Background(), 2*time.Second)
	}); err != nil {
		t.Fatal(err)
	}
	if !got.Equal(message) || !providerDelivery.acked.Load() || providerDelivery.nacked.Load() || providerDelivery.extended.Load() != int64(2*time.Second) {
		t.Fatalf("got=%v ack=%v nack=%v extend=%v", got, providerDelivery.acked.Load(), providerDelivery.nacked.Load(), providerDelivery.extended.Load())
	}
	_ = subscription.Close(context.Background())
	_ = publisher.Close(context.Background())
	if !receiver.stopped.Load() || !topic.stopped.Load() {
		t.Fatal("provider handles were not stopped")
	}
}

func TestHandlerErrorNacks(t *testing.T) {
	message := testProviderMessage(t)
	providerDelivery := &fakeProviderDelivery{message: message}
	subscription, err := NewSubscription(Config{Topic: "control.events"}, &fakeReceiver{delivery: providerDelivery})
	if err != nil {
		t.Fatal(err)
	}
	if err := subscription.Receive(context.Background(), func(context.Context, messaging.Delivery) error { return context.Canceled }); err != nil {
		t.Fatal(err)
	}
	if !providerDelivery.nacked.Load() || providerDelivery.acked.Load() {
		t.Fatal("delivery was not negatively acknowledged")
	}
}

func testProviderMessage(t *testing.T) PublishMessage {
	t.Helper()
	now := time.Now().UTC()
	id, err := identifiers.NewIDAt(messaging.MessageIDKind, now)
	if err != nil {
		t.Fatal(err)
	}
	message, err := messaging.NewMessage(id, "control.events", "", "application/json", []byte("{}"), nil, requestmeta.Metadata{}, now)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encode(message)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

var _ = sync.Mutex{}

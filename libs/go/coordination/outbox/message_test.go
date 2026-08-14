// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package outbox

import (
	"encoding/json"
	"testing"
	"time"

	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/requestmeta"
)

func TestMessageDefensiveCopiesAndRoundTrip(t *testing.T) {
	identifier, err := identifiers.NewID(MessageIDKind)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Now().UTC().Round(0)
	payload := []byte(`{"run_id":"run-example"}`)
	headers := map[string]string{"schema-version": "1"}
	message, err := NewMessage(identifier, "orchestration.run.created", "tenant-a", "application/json", payload, headers, requestmeta.Metadata{}, createdAt, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = 'x'
	headers["schema-version"] = "mutated"
	if string(message.Payload()) != `{"run_id":"run-example"}` {
		t.Fatalf("payload was not copied: %q", message.Payload())
	}
	if message.Headers()["schema-version"] != "1" {
		t.Fatalf("headers were not copied: %v", message.Headers())
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) == 0 || message.AvailableAt() != createdAt {
		t.Fatalf("invalid encoded message or default availability: %s", encoded)
	}
}

func TestMessageRejectsInvalidEnvelope(t *testing.T) {
	identifier, err := identifiers.NewID(MessageIDKind)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	cases := []struct {
		name        string
		topic       string
		contentType string
		payload     []byte
		createdAt   time.Time
		availableAt time.Time
	}{
		{name: "topic", topic: "Run.Created", contentType: "application/json", payload: []byte("{}"), createdAt: now, availableAt: now},
		{name: "content type", topic: "run.created", contentType: "", payload: []byte("{}"), createdAt: now, availableAt: now},
		{name: "payload", topic: "run.created", contentType: "application/json", payload: nil, createdAt: now, availableAt: now},
		{name: "timestamps", topic: "run.created", contentType: "application/json", payload: []byte("{}"), createdAt: now, availableAt: now.Add(-time.Second)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewMessage(identifier, test.topic, "", test.contentType, test.payload, nil, requestmeta.Metadata{}, test.createdAt, test.availableAt); err == nil {
				t.Fatal("invalid message accepted")
			}
		})
	}
}

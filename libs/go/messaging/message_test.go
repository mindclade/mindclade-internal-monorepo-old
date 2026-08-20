// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package messaging_test

import (
	"testing"
	"time"

	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/messaging"
	"go.mindclade.dev/libs/go/requestmeta"
)

func TestMessageDefensiveCopies(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	id, err := identifiers.NewIDAt(messaging.MessageIDKind, now)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("payload")
	attributes := map[string]string{"schema": "v1"}
	message, err := messaging.NewMessage(id, "runs.created", "tenant", "application/protobuf", payload, attributes, requestmeta.Metadata{}, now)
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = 'x'
	attributes["schema"] = "v2"
	if string(message.Payload()) != "payload" || message.Attributes()["schema"] != "v1" {
		t.Fatal("message retained mutable input")
	}
	returned := message.Payload()
	returned[0] = 'x'
	if string(message.Payload()) != "payload" {
		t.Fatal("payload accessor leaked mutable state")
	}
}

func TestMessageRejectsNoncanonicalTopic(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	id, _ := identifiers.NewIDAt(messaging.MessageIDKind, now)
	if _, err := messaging.NewMessage(id, "Runs.Created", "", "application/json", []byte("{}"), nil, requestmeta.Metadata{}, now); err == nil {
		t.Fatal("expected invalid topic")
	}
}

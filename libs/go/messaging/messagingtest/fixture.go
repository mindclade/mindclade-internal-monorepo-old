// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package messagingtest

import (
	"time"

	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/messaging"
	"mindclade.internal/libs/go/requestmeta"
)

// Message returns a deterministic valid fixture.
func Message(now time.Time) messaging.Message {
	id, err := identifiers.NewIDAt(messaging.MessageIDKind, now)
	if err != nil {
		panic(err)
	}
	value, err := messaging.NewMessage(
		id,
		"runs.created",
		"tenant-1",
		"application/protobuf",
		[]byte("payload"),
		map[string]string{"schema": "mindclade.run.v1"},
		requestmeta.Metadata{},
		now,
	)
	if err != nil {
		panic(err)
	}
	return value
}

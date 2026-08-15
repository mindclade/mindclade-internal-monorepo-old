// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outboxtest

import (
	"testing"
	"time"

	canonical "mindclade.internal/libs/go/coordination/outbox/outboxtest"
	storageoutbox "mindclade.internal/libs/go/storage/outbox"
)

type Factory = canonical.Factory
type Clock = canonical.Clock

func Run(t *testing.T, factory Factory, valueClock Clock) {
	canonical.Run(t, factory, valueClock)
}

func Envelope(t *testing.T, now time.Time, topic string, payload []byte) storageoutbox.Envelope {
	return canonical.Message(t, now, topic, payload)
}

func Message(t *testing.T, now time.Time, topic string, payload []byte) storageoutbox.Message {
	return canonical.Message(t, now, topic, payload)
}

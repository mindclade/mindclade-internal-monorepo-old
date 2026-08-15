// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package messaging

import (
	"context"
	"time"
)

// Handler receives one delivery. Returning nil acknowledges the delivery;
// returning an error negatively acknowledges it. Consumers that need atomic
// side effects must pair the handler with a durable inbox transaction.
type Handler func(context.Context, Delivery) error

// Delivery exposes broker delivery metadata and explicit settlement. Handler
// users normally return an error and allow Subscription.Receive to settle it;
// manual settlement is available for streaming or transaction-aware adapters.
type Delivery interface {
	Message() Message
	Attempt() int
	Deadline() time.Time
	Ack(context.Context) error
	Nack(context.Context) error
	Extend(context.Context, time.Duration) error
	Settled() bool
}

// Subscription owns a receive loop until ctx cancellation or terminal provider
// failure. Implementations must bound concurrent handlers and in-memory queues.
type Subscription interface {
	Receive(context.Context, Handler) error
	Close(context.Context) error
}

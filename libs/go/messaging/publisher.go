// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package messaging

import (
	"context"
	"time"

	"go.mindclade.dev/libs/go/identifiers"
)

// Publication is provider acknowledgement of a publish operation.
type Publication struct {
	MessageID   identifiers.ID
	ProviderID  string
	PublishedAt time.Time
}

// Publisher publishes one immutable message. A successful return means the
// provider accepted responsibility for delivery, not that a consumer handled it.
type Publisher interface {
	Publish(context.Context, Message) (Publication, error)
}

// PublisherFunc adapts a function to Publisher.
type PublisherFunc func(context.Context, Message) (Publication, error)

func (function PublisherFunc) Publish(ctx context.Context, message Message) (Publication, error) {
	if function == nil {
		return Publication{}, unavailable(ErrPublishFailed, "nil_publisher", "messaging.Publisher.Publish", nil)
	}
	return function(ctx, message)
}

// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package outbox

import "context"

type Publisher interface {
	Publish(context.Context, Message) error
}

type PublisherFunc func(context.Context, Message) error

func (function PublisherFunc) Publish(ctx context.Context, message Message) error {
	if function == nil {
		return ErrPublishFailed
	}
	return function(ctx, message)
}

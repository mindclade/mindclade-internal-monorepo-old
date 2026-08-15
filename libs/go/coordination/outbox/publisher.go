// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

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

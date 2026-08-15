// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package lease

import (
	"context"
	"time"
)

type Store interface {
	Acquire(context.Context, AcquireRequest) (Lease, error)
	Renew(context.Context, Lease, time.Duration) (Lease, error)
	Release(context.Context, Lease) error
	Lookup(context.Context, Key) (Lease, error)
}

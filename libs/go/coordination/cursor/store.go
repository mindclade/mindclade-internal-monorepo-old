// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package cursor

import "context"

type Store interface {
	Load(context.Context, Key) (Cursor, error)
	Advance(context.Context, AdvanceRequest) (Cursor, error)
	Delete(context.Context, Key, uint64) error
}

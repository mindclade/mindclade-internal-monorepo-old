// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package cache

import "context"

type Store interface {
	Get(context.Context, Key) (Entry, error)
	Set(context.Context, Key, []byte, SetOptions) (Entry, error)
	Delete(context.Context, Key, DeleteOptions) error
}

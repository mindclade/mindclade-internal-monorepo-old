// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outbox

import coordination "go.mindclade.dev/libs/go/coordination/outbox"

// Repository atomically appends envelopes and owns fenced publication claims.
type Repository = coordination.Store

// Store is retained for source compatibility with the coordination package.
type Store = coordination.Store

// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package outbox

import coordination "mindclade.internal/libs/go/coordination/outbox"

// Repository atomically appends envelopes and owns fenced publication claims.
type Repository = coordination.Store

// Store is retained for source compatibility with the coordination package.
type Store = coordination.Store

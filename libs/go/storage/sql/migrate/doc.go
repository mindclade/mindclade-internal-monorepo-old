// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package migrate provides the repository-wide PostgreSQL schema migration
// mechanism. Migrations are immutable, checksummed, monotonically versioned,
// serialized by a session-scoped advisory lock, and applied one transaction at
// a time. Service-specific SQL remains with the owning deployable.
package migrate

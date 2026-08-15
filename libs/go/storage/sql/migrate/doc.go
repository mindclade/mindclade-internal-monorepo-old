// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package migrate provides the repository-wide PostgreSQL schema migration
// mechanism. Migrations are immutable, checksummed, monotonically versioned,
// serialized by a session-scoped advisory lock, and applied one transaction at
// a time. Service-specific SQL remains with the owning deployable.
package migrate

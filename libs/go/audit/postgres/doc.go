// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package postgres provides the canonical transaction-aware PostgreSQL audit
// recorder. It stores the complete immutable audit event together with a small
// query index and rejects conflicting reuse of an audit event identifier.
package postgres

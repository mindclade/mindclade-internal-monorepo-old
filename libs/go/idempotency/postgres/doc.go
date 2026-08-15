// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package postgres implements the idempotency.Store contract with PostgreSQL.
//
// The adapter is transaction-aware: when ctx contains a transaction installed
// by storage/sql/transaction, every operation uses that transaction. This lets
// callers make idempotency acquisition, domain writes, transactional-outbox
// inserts, and completion one atomic database commit.
package postgres

// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package postgres implements the transactional outbox contract on PostgreSQL.
// Append joins a transaction carried by storage/sql/transaction; claims use
// database time, SKIP LOCKED, lease expiration, and token/version fencing.
package postgres

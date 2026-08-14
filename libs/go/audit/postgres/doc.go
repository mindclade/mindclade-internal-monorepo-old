// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package postgres provides the canonical transaction-aware PostgreSQL audit
// recorder. It stores the complete immutable audit event together with a small
// query index and rejects conflicting reuse of an audit event identifier.
package postgres

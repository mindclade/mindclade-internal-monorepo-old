// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package inbox implements the transactional inbox pattern by composing the
// existing idempotency contract with a caller-supplied transaction boundary.
// It deliberately owns no broker, event schema, or domain projection policy.
package inbox

// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package projector provides the standard bounded event-projection loop. It
// composes transactional inbox processing and fenced cursors while leaving
// broker access, event schemas, and domain projections to consumers.
package projector

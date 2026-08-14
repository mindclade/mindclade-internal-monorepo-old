// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

// Package outbox provides a transactional outbox contract, fenced delivery
// claims, and a servicekit-managed dispatcher for durable event publication.
//
// Message insertion participates in storage/sql/transaction when a PostgreSQL
// adapter is used. Delivery is at-least-once; publishers and consumers must be
// idempotent. A stale dispatcher cannot acknowledge, reschedule, or dead-letter
// a message after its claim has been renewed or reacquired.
package outbox

// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package messaging defines the narrow, transport-neutral delivery mechanics
// shared by outbox dispatchers, ingestion coordinators, projectors, and
// administrative processors.
//
// Messaging deliberately does not define domain event schemas, workflow
// policy, or exactly-once semantics. Producers publish immutable messages;
// consumers receive at-least-once deliveries and must use durable inboxes or
// another explicit idempotency mechanism around side effects.
package messaging

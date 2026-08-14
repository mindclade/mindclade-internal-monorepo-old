// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package coordination contains the small durable coordination mechanisms
// shared by Mindclade control-plane processes.
//
// The package owns only cross-service execution mechanics: fenced claims,
// persisted failure summaries, outboxes, inboxes, leased work queues,
// projection checkpoints, and leader leases. Tenancy, quotas, scheduling,
// scientific policy, model semantics, and product workflows remain in their
// owning control-plane or domain packages.
package coordination

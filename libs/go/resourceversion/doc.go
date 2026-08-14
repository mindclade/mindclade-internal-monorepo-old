// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package resourceversion defines transport-neutral optimistic-concurrency
// versions and write preconditions for durable control-plane resources.
//
// The package deliberately does not know about any domain resource, SQL query,
// HTTP framework, or protobuf message. Domain repositories map Version to their
// own version column, while transport adapters map it to ETag/If-Match or
// generated protocol fields.
package resourceversion

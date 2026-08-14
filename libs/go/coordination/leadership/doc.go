// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package leadership provides lease-backed leader election with fencing,
// bounded renewal, servicekit lifecycle integration, and explicit leadership
// loss semantics. It is suitable for singleton schedulers, reconcilers,
// projectors, dispatch coordinators, and administrative maintenance loops.
package leadership

// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

// Package ownerrefs provides validated wrappers around controller-runtime
// owner-reference utilities. It mutates objects in memory; callers remain
// responsible for persisting the desired object with an optimistic patch.
package ownerrefs

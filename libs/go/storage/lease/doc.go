// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

// Package lease defines fenced, renewable ownership records. A token and
// version must accompany renew and release operations so stale owners cannot
// mutate a lease acquired by a newer process.
package lease

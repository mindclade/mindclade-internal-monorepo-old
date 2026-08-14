// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

// Package redis implements cache.Store with Redis using atomic Lua scripts for
// compare-and-swap versioning and TTL changes.
package redis

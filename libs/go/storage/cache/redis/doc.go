// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package redis implements cache.Store with Redis using atomic Lua scripts for
// compare-and-swap versioning and TTL changes.
package redis

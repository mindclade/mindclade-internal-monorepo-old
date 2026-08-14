// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package config implements strict, provenance-carrying service configuration.
// Sources are merged in explicit order, unknown keys fail closed, secrets are
// redacted, and resolved snapshots carry deterministic digests.
package config

// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package outbound provides the single hardened HTTP client construction path
// for webhooks, ingestion connectors, partner callbacks, and external
// qualification submissions. It validates every initial and redirected URL,
// resolves and checks every dial target, bounds response bodies, and disables
// ambient proxy use unless explicitly configured.
package outbound

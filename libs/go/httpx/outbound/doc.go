// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package outbound provides the single hardened HTTP client construction path
// for webhooks, ingestion connectors, partner callbacks, and external
// qualification submissions. It validates every initial and redirected URL,
// resolves and checks every dial target, bounds response bodies, and disables
// ambient proxy use unless explicitly configured.
package outbound

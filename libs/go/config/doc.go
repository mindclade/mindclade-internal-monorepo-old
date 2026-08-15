// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package config implements strict, provenance-carrying service configuration.
// Sources are merged in explicit order, unknown keys fail closed, secrets are
// redacted, and resolved snapshots carry deterministic digests.
package config

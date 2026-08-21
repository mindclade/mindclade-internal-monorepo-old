// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package admission owns global, policy-versioned admission and quota
// reservations for external AI Gateway calls. It deliberately does not own
// provider credentials, request payloads, model releases, or node-local
// scheduling decisions.
package admission

// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package inbox implements the transactional inbox pattern by composing the
// existing idempotency contract with a caller-supplied transaction boundary.
// It deliberately owns no broker, event schema, or domain projection policy.
package inbox

// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package projector provides the standard bounded event-projection loop. It
// composes transactional inbox processing and fenced cursors while leaving
// broker access, event schemas, and domain projections to consumers.
package projector

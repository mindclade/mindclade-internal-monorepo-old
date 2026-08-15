// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package ownerrefs provides validated wrappers around controller-runtime
// owner-reference utilities. It mutates objects in memory; callers remain
// responsible for persisting the desired object with an optimistic patch.
package ownerrefs

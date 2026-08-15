// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package lease defines fenced, renewable ownership records. A token and
// version must accompany renew and release operations so stale owners cannot
// mutate a lease acquired by a newer process.
package lease

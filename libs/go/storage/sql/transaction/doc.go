// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package transaction provides panic-safe database/sql transaction execution.
// It does not automatically retry callbacks because they may perform external
// side effects; retry decisions remain at the owning application boundary.
package transaction

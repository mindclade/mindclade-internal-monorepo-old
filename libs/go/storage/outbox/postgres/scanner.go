// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

// RowScanner is the minimal database row contract used by storage adapters.
// It is exposed for integration and test doubles without coupling callers to
// *sql.Row or *sql.Rows.
type RowScanner interface {
	Scan(dest ...any) error
}

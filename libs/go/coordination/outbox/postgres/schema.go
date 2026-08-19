// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import _ "embed"

//go:embed migrations/000001_outbox.up.sql
var schema string

// Schema returns the forward-only DDL this adapter requires. Composition roots
// own migration ordering, so the version and name are assigned by the caller
// when the statement is placed into a storage/sql/migrate manifest.
func Schema() string { return schema }

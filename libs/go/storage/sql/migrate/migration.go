// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"go.mindclade.dev/libs/go/faults"
)

const MaximumMigrationNameBytes = 128

// Migration is one immutable forward-only schema change. Destructive changes
// must follow expand/contract release policy rather than relying on Down SQL.
type Migration struct {
	Version uint64
	Name    string
	Up      string
}

func (migration Migration) Validate() error {
	name := strings.TrimSpace(migration.Name)
	if migration.Version == 0 || name == "" || name != migration.Name || len(name) > MaximumMigrationNameBytes || strings.TrimSpace(migration.Up) == "" {
		return invalid(ErrInvalidMigration, "invalid_migration_definition", faults.Fields{"version": migration.Version, "name": migration.Name})
	}
	for _, character := range name {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_' || character == '-' {
			continue
		}
		return invalid(ErrInvalidMigration, "invalid_migration_name", faults.Fields{"version": migration.Version, "name": migration.Name})
	}
	return nil
}

func (migration Migration) Checksum() string {
	digest := sha256.Sum256([]byte(migration.Up))
	return hex.EncodeToString(digest[:])
}

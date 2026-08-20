// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package migrate

import (
	"sort"

	"go.mindclade.dev/libs/go/faults"
)

// Manifest validates and owns a deterministic migration sequence.
type Manifest struct {
	migrations []Migration
	byVersion  map[uint64]Migration
}

func NewManifest(migrations ...Migration) (Manifest, error) {
	copied := append([]Migration(nil), migrations...)
	sort.Slice(copied, func(left, right int) bool { return copied[left].Version < copied[right].Version })
	byVersion := make(map[uint64]Migration, len(copied))
	for index, migration := range copied {
		if err := migration.Validate(); err != nil {
			return Manifest{}, err
		}
		if _, exists := byVersion[migration.Version]; exists {
			return Manifest{}, invalid(ErrInvalidMigration, "duplicate_migration_version", faults.Fields{"version": migration.Version})
		}
		if index > 0 && copied[index-1].Version >= migration.Version {
			return Manifest{}, invalid(ErrInvalidMigration, "non_monotonic_migration_versions", nil)
		}
		byVersion[migration.Version] = migration
	}
	return Manifest{migrations: copied, byVersion: byVersion}, nil
}

func (manifest Manifest) Migrations() []Migration {
	return append([]Migration(nil), manifest.migrations...)
}
func (manifest Manifest) lookup(version uint64) (Migration, bool) {
	value, ok := manifest.byVersion[version]
	return value, ok
}

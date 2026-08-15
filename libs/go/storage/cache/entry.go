// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package cache

import "time"

type Entry struct {
	Key       Key
	Value     []byte
	Version   uint64
	ExpiresAt time.Time
}

func (entry Entry) Clone() Entry { entry.Value = append([]byte(nil), entry.Value...); return entry }
func (entry Entry) Expired(now time.Time) bool {
	return !entry.ExpiresAt.IsZero() && !now.Before(entry.ExpiresAt)
}
func (entry Entry) Validate() error {
	if err := entry.Key.Validate(); err != nil {
		return err
	}
	if entry.Version == 0 {
		return invalidEntry("cache entry version must be positive", "invalid_cache_version")
	}
	return nil
}

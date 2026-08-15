// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package config

import (
	"sort"
	"time"

	"mindclade.internal/libs/go/identifiers"
)

type Origin struct {
	Source     string
	Secret     bool
	Reloadable bool
}
type Snapshot struct {
	values   map[string]string
	origins  map[string]Origin
	digest   identifiers.Digest
	loadedAt time.Time
}

func (snapshot Snapshot) Digest() identifiers.Digest { return snapshot.digest }
func (snapshot Snapshot) LoadedAt() time.Time        { return snapshot.loadedAt }
func (snapshot Snapshot) Get(key string) (string, bool) {
	value, ok := snapshot.values[key]
	return value, ok
}
func (snapshot Snapshot) MustGet(key string) string {
	value, ok := snapshot.Get(key)
	if !ok {
		panic("config: missing key " + key)
	}
	return value
}
func (snapshot Snapshot) Origin(key string) (Origin, bool) {
	value, ok := snapshot.origins[key]
	return value, ok
}
func (snapshot Snapshot) Keys() []string {
	keys := make([]string, 0, len(snapshot.values))
	for key := range snapshot.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func (snapshot Snapshot) Values() map[string]string {
	result := make(map[string]string, len(snapshot.values))
	for key, value := range snapshot.values {
		result[key] = value
	}
	return result
}
func (snapshot Snapshot) Redacted() map[string]string {
	result := make(map[string]string, len(snapshot.values))
	for key, value := range snapshot.values {
		if snapshot.origins[key].Secret && value != "" {
			result[key] = "[REDACTED]"
		} else {
			result[key] = value
		}
	}
	return result
}
func (snapshot Snapshot) Equal(other Snapshot) bool { return snapshot.digest.Equal(other.digest) }

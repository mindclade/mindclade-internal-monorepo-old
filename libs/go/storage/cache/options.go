// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package cache

import "time"

type SetOptions struct {
	TTL       time.Duration
	IfAbsent  bool
	IfVersion *uint64
}

func (options SetOptions) Validate() error {
	if options.TTL < 0 || options.IfAbsent && options.IfVersion != nil || options.IfVersion != nil && *options.IfVersion == 0 {
		return invalidOptions("invalid cache set options", "invalid_cache_set_options")
	}
	return nil
}

type DeleteOptions struct{ IfVersion *uint64 }

func (options DeleteOptions) Validate() error {
	if options.IfVersion != nil && *options.IfVersion == 0 {
		return invalidOptions("invalid cache delete options", "invalid_cache_delete_options")
	}
	return nil
}

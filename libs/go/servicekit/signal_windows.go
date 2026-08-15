// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//go:build windows

package servicekit

import "os"

// DefaultSignals returns the conventional graceful termination signals for
// Windows services.
func DefaultSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

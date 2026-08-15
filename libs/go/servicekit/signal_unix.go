// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package servicekit

import (
	"os"
	"syscall"
)

// DefaultSignals returns the conventional graceful termination signals for
// Unix services.
func DefaultSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

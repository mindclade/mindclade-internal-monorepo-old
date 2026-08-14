// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

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

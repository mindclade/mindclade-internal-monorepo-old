// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

//go:build windows

package servicekit

import "os"

// DefaultSignals returns the conventional graceful termination signals for
// Windows services.
func DefaultSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

// Package clock provides injectable real and deterministic clocks for
// Mindclade Go services and libraries.
//
// Production code should accept the Clock interface at construction time and
// use RealClock in normal execution. Tests can use FakeClock and explicitly
// advance time without sleeping or depending on wall-clock scheduling.
//
// The package deliberately depends only on the Go standard library. It does
// not own retry policy, job scheduling, cron expressions, or service lifecycle
// management.
package clock

// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package outboxtest

import (
	mcclock "mindclade.internal/libs/go/clock"
	canonicalmemory "mindclade.internal/libs/go/coordination/outbox/memory"
	"mindclade.internal/libs/go/storage/lease"
)

type MemoryStore = canonicalmemory.Store
type MemoryOption = canonicalmemory.Option

func NewMemory(options ...MemoryOption) (*MemoryStore, error) {
	return canonicalmemory.New(options...)
}

func WithClock(value mcclock.Clock) MemoryOption {
	return canonicalmemory.WithClock(value)
}

func WithTokenGenerator(value func() (lease.Token, error)) MemoryOption {
	return canonicalmemory.WithTokenGenerator(value)
}

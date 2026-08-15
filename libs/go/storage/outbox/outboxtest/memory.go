// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package outboxtest

import (
	mcclock "go.mindclade.dev/libs/go/clock"
	canonicalmemory "go.mindclade.dev/libs/go/coordination/outbox/memory"
	"go.mindclade.dev/libs/go/storage/lease"
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

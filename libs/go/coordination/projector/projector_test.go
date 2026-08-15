// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package projector_test

import (
	"context"
	"mindclade.internal/libs/go/coordination/cursor"
	cursormemory "mindclade.internal/libs/go/coordination/cursor/memory"
	"mindclade.internal/libs/go/coordination/inbox"
	"mindclade.internal/libs/go/coordination/projector"
	"mindclade.internal/libs/go/idempotency"
	"mindclade.internal/libs/go/idempotency/idempotencytest"
	"mindclade.internal/libs/go/identifiers"
	"sync"
	"testing"
	"time"
)

type source struct {
	mu     sync.Mutex
	events []projector.Event
}

func (s *source) Fetch(context.Context, *cursor.Cursor, int) ([]projector.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.events
	s.events = nil
	return events, nil
}
func TestProjectsAndAdvances(t *testing.T) {
	scope, _ := idempotency.ParseScope("projector.registry")
	key, _ := idempotency.ParseKey("event-00000001")
	identity, _ := idempotency.NewIdentity(scope, key)
	src := &source{events: []projector.Event{{Identity: identity, Fingerprint: identifiers.SHA256([]byte("one")), Sequence: 1}}}
	idem, _ := idempotencytest.NewMemoryStore()
	inboxProcessor, _ := inbox.New(inbox.RunnerFunc(func(ctx context.Context, work func(context.Context) error) error { return work(ctx) }), idem)
	cursors := cursormemory.New()
	cursorKey, _ := cursor.NewKey("projector", "registry")
	applied := 0
	processor, err := projector.New(src, projector.HandlerFunc(func(context.Context, projector.Event) (idempotency.Result, error) {
		applied++
		return idempotency.EmptyResult()
	}), inboxProcessor, cursors, projector.FenceProviderFunc(func() (uint64, bool) { return 1, true }), projector.Config{Cursor: cursorKey, PollInterval: time.Millisecond, BatchSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_ = processor.Run(ctx)
	value, err := cursors.Load(context.Background(), cursorKey)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || value.Sequence != 1 {
		t.Fatalf("applied=%d cursor=%+v", applied, value)
	}
}

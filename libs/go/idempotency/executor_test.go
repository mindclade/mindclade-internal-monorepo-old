// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package idempotency_test

import (
	"context"
	"errors"
	mcclock "go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/idempotency"
	"go.mindclade.dev/libs/go/idempotency/idempotencytest"
	"go.mindclade.dev/libs/go/identifiers"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*idempotency.Executor, idempotency.AcquireRequest, *mcclock.FakeClock) {
	t.Helper()
	now := time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC)
	clock := mcclock.NewFake(now)
	generator, err := identifiers.NewGenerator(identifiers.WithTimeSource(clock.Now), identifiers.WithEntropySource(strings.NewReader(strings.Repeat("z", 8192))))
	if err != nil {
		t.Fatal(err)
	}
	store, err := idempotencytest.NewMemoryStore(idempotencytest.WithClock(clock), idempotencytest.WithGenerator(generator))
	if err != nil {
		t.Fatal(err)
	}
	executor, err := idempotency.NewExecutor(store, idempotency.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := idempotency.NewIdentity(idempotency.MustParseScope("control/runs.create"), idempotency.MustParseKey("request-123456"))
	if err != nil {
		t.Fatal(err)
	}
	return executor, idempotency.AcquireRequest{Identity: identity, Fingerprint: identifiers.SHA256String("canonical-request"), TTL: time.Hour, LeaseDuration: time.Minute}, clock
}
func TestExecuteAndReplay(t *testing.T) {
	executor, request, _ := fixture(t)
	calls := 0
	operation := func(context.Context) (idempotency.Result, error) {
		calls++
		return idempotency.NewResult([]byte("created"), "text/plain", nil)
	}
	first, err := executor.Execute(context.Background(), request, operation)
	if err != nil {
		t.Fatal(err)
	}
	second, err := executor.Execute(context.Background(), request, operation)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || first.Replayed() || !first.Executed() || !first.Committed() || !second.Replayed() || string(second.Result().Payload()) != "created" {
		t.Fatalf("calls=%d first=%+v second=%+v", calls, first, second)
	}
}
func TestOperationFailureReleasesLease(t *testing.T) {
	executor, request, _ := fixture(t)
	failure := faults.New(faults.CodeUnavailable, "backend unavailable", faults.WithReason("backend_unavailable"))
	_, err := executor.Execute(context.Background(), request, func(context.Context) (idempotency.Result, error) { return idempotency.Result{}, failure })
	if !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
	calls := 0
	_, err = executor.Execute(context.Background(), request, func(context.Context) (idempotency.Result, error) { calls++; return idempotency.NewResult(nil, "", nil) })
	if err != nil || calls != 1 {
		t.Fatalf("retry err=%v calls=%d", err, calls)
	}
}
func TestConflict(t *testing.T) {
	executor, request, _ := fixture(t)
	_, err := executor.Execute(context.Background(), request, func(context.Context) (idempotency.Result, error) { return idempotency.NewResult([]byte("ok"), "", nil) })
	if err != nil {
		t.Fatal(err)
	}
	request.Fingerprint = identifiers.SHA256String("different")
	_, err = executor.Execute(context.Background(), request, func(context.Context) (idempotency.Result, error) { return idempotency.NewResult(nil, "", nil) })
	if !faults.IsReason(err, idempotency.ReasonKeyConflict) {
		t.Fatalf("error=%v", err)
	}
}

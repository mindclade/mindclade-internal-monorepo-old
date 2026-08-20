// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package retry

import (
	"context"
	"errors"
	"testing"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
)

func TestConstructionContracts(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	if _, err := NewExecutor(policy, WithClock(nil)); !errors.Is(err, ErrNilClock) || faults.CodeOf(err) != faults.CodeInvalidArgument {
		t.Fatalf("nil clock error = %v", err)
	}

	var typedNilClock *clock.FakeClock
	if _, err := NewExecutor(policy, WithClock(typedNilClock)); !errors.Is(err, ErrNilClock) {
		t.Fatalf("typed nil clock error = %v", err)
	}

	var typedNilClassifier ClassifierFunc
	if _, err := NewExecutor(policy, WithClassifier(typedNilClassifier)); !errors.Is(err, ErrNilClassifier) {
		t.Fatalf("typed nil classifier error = %v", err)
	}

	var typedNilRandom RandomSourceFunc
	if _, err := NewExecutor(policy, WithRandomSource(typedNilRandom)); !errors.Is(err, ErrNilRandomSource) {
		t.Fatalf("typed nil random error = %v", err)
	}
}

func TestExecutionArgumentContracts(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Do(nil, "operation", func(context.Context, Attempt) error { return nil }); !errors.Is(err, ErrNilContext) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := executor.Do(context.Background(), "operation", nil); !errors.Is(err, ErrNilOperation) {
		t.Fatalf("nil operation error = %v", err)
	}
	if _, err := executor.Do(context.Background(), " bad name ", func(context.Context, Attempt) error { return nil }); !errors.Is(err, ErrInvalidOperationName) {
		t.Fatalf("invalid name error = %v", err)
	}
	var nilExecutor *Executor
	if _, err := nilExecutor.Do(context.Background(), "operation", func(context.Context, Attempt) error { return nil }); !errors.Is(err, ErrNilExecutor) {
		t.Fatalf("nil executor error = %v", err)
	}
}

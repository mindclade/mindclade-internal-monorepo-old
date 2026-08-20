// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/faults"
)

func TestDefaultSignalsReturnsCopy(t *testing.T) {
	t.Parallel()

	first := DefaultSignals()
	second := DefaultSignals()
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("DefaultSignals returned an empty slice")
	}
	first[0] = nil
	if second[0] == nil {
		t.Fatal("DefaultSignals reused mutable backing storage")
	}
}

func TestSignalContextFollowsParent(t *testing.T) {
	t.Parallel()

	parent, cancelParent := context.WithCancel(context.Background())
	ctx, stop, err := SignalContext(parent)
	if err != nil {
		t.Fatalf("SignalContext returned %v", err)
	}
	defer stop()

	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("signal context did not follow parent cancellation")
	}
}

func TestSignalContextRejectsNilParent(t *testing.T) {
	t.Parallel()

	ctx, stop, err := SignalContext(nil)
	if ctx != nil || stop != nil || !errors.Is(err, ErrNilContext) {
		t.Fatalf("SignalContext(nil) = (%v, %v, %v)", ctx, stop, err)
	}
	if faults.CodeOf(err) != faults.CodeInvalidArgument || faults.ReasonOf(err) != "nil_context" {
		t.Fatalf("classification = %s/%q", faults.CodeOf(err), faults.ReasonOf(err))
	}
}

func TestRunWithSignalsFollowsParentCancellation(t *testing.T) {
	t.Parallel()

	service, err := New("signal-service")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Add(Component{Name: "passive", Start: func(context.Context) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- service.RunWithSignals(parent) }()

	deadline := time.After(time.Second)
	for service.Snapshot().State != StateRunning {
		select {
		case <-deadline:
			t.Fatal("service did not become running")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("RunWithSignals returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunWithSignals did not return")
	}
}

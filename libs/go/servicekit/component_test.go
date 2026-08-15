// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/faults"
)

func TestValidateName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"api",
		"control-plane",
		"worker_1",
		"database.primary",
		"component/cache",
		"API2",
	} {
		if err := validateName("component", name, operationAdd); err != nil {
			t.Fatalf("validateName(%q) returned %v", name, err)
		}
	}

	for _, name := range []string{
		"",
		" api",
		"api ",
		"-api",
		"api?",
		strings.Repeat("a", maxNameLength+1),
	} {
		err := validateName("component", name, operationAdd)
		if !errors.Is(err, ErrInvalidName) {
			t.Fatalf("validateName(%q) error = %v, want ErrInvalidName", name, err)
		}
		if !faults.IsCode(err, faults.CodeInvalidArgument) {
			t.Fatalf("validateName(%q) code = %s", name, faults.CodeOf(err))
		}
		if got := faults.ReasonOf(err); got != "invalid_component_name" {
			t.Fatalf("validateName(%q) reason = %q", name, got)
		}
	}
}

func TestComponentValidation(t *testing.T) {
	t.Parallel()

	empty := Component{Name: "empty"}
	err := empty.validate()
	if !errors.Is(err, ErrNilComponent) {
		t.Fatalf("empty component error = %v, want ErrNilComponent", err)
	}
	if !faults.IsCode(err, faults.CodeInvalidArgument) || faults.ReasonOf(err) != "empty_component" {
		t.Fatalf("empty component classification = %s/%q", faults.CodeOf(err), faults.ReasonOf(err))
	}

	valid := Component{Name: "worker", Drain: func(context.Context) error { return nil }, Run: func(context.Context) error { return nil }}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid component error = %v", err)
	}
}

func TestInvokeRecoversPanic(t *testing.T) {
	t.Parallel()

	err := invoke(context.Background(), func(context.Context) error {
		panic("boom")
	})

	var panicErr *PanicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("invoke error = %T %v, want *PanicError", err, err)
	}
	if panicErr.Value != "boom" {
		t.Fatalf("panic value = %#v, want boom", panicErr.Value)
	}
	if faults.CodeOf(err) != faults.CodeInternal || faults.ReasonOf(err) != "panic_recovered" {
		t.Fatalf("panic classification = %s/%q", faults.CodeOf(err), faults.ReasonOf(err))
	}
	if got := faults.PublicMessageOf(err); strings.Contains(got, "boom") {
		t.Fatalf("public panic message leaked value: %q", got)
	}
	stack := panicErr.Stack()
	if len(stack) == 0 {
		t.Fatal("panic stack is empty")
	}
	stack[0] = 0
	if panicErr.Stack()[0] == 0 {
		t.Fatal("PanicError.Stack did not return a defensive copy")
	}
}

func TestInvokeBoundedHonorsDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := invokeBounded(ctx, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("invokeBounded error = %v, want context deadline exceeded", err)
	}
}

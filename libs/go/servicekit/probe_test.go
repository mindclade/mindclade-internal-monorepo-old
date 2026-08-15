// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"mindclade.internal/libs/go/faults"
)

func TestProbeSetRegistrationAndReport(t *testing.T) {
	t.Parallel()

	expectedFailure := errors.New("dependency unavailable")
	set, err := NewProbeSet(time.Second)
	if err != nil {
		t.Fatalf("NewProbeSet returned %v", err)
	}
	if err := set.Register("zeta", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("Register zeta returned %v", err)
	}
	if err := set.Register("alpha", func(context.Context) error { return expectedFailure }); err != nil {
		t.Fatalf("Register alpha returned %v", err)
	}
	if err := set.Register("alpha", func(context.Context) error { return nil }); !errors.Is(err, ErrDuplicateProbe) {
		t.Fatalf("duplicate Register error = %v, want ErrDuplicateProbe", err)
	} else if faults.CodeOf(err) != faults.CodeAlreadyExists {
		t.Fatalf("duplicate code = %s", faults.CodeOf(err))
	}

	if got, want := set.Names(), []string{"alpha", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}

	report := set.Check(context.Background())
	if report.OK {
		t.Fatal("report.OK = true, want false")
	}
	if len(report.Results) != 2 || report.Results[0].Name != "alpha" || report.Results[1].Name != "zeta" {
		t.Fatalf("unexpected sorted results: %+v", report.Results)
	}
	reportErr := report.Err()
	if !errors.Is(reportErr, expectedFailure) {
		t.Fatalf("report.Err() = %v, want wrapped dependency failure", reportErr)
	}
	if faults.CodeOf(reportErr) != faults.CodeUnavailable {
		t.Fatalf("report code = %s", faults.CodeOf(reportErr))
	}
	var failures *ProbeFailures
	if !errors.As(reportErr, &failures) || !reflect.DeepEqual(failures.Names(), []string{"alpha"}) {
		t.Fatalf("unexpected ProbeFailures: %#v", failures)
	}

	if err := set.Replace("alpha", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("Replace returned %v", err)
	}
	if report := set.Check(context.Background()); !report.OK || report.Err() != nil {
		t.Fatalf("replaced report = %+v, err = %v", report, report.Err())
	}

	if !set.Unregister("zeta") || set.Unregister("zeta") {
		t.Fatal("Unregister did not report existence correctly")
	}
}

func TestProbeSetTimeoutAndPanic(t *testing.T) {
	t.Parallel()

	set, err := NewProbeSet(20 * time.Millisecond)
	if err != nil {
		t.Fatalf("NewProbeSet returned %v", err)
	}
	if err := set.Register("timeout", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatalf("Register timeout returned %v", err)
	}
	if err := set.Register("panic", func(context.Context) error {
		panic("probe panic")
	}); err != nil {
		t.Fatalf("Register panic returned %v", err)
	}

	report := set.Check(context.Background())
	if report.OK {
		t.Fatal("report.OK = true, want false")
	}

	var foundTimeout, foundPanic bool
	for _, result := range report.Results {
		switch result.Name {
		case "timeout":
			foundTimeout = errors.Is(result.Err, context.DeadlineExceeded) &&
				faults.CodeOf(result.Err) == faults.CodeDeadlineExceeded &&
				faults.ReasonOf(result.Err) == "probe_timeout"
		case "panic":
			var panicErr *PanicError
			foundPanic = errors.As(result.Err, &panicErr) &&
				faults.CodeOf(result.Err) == faults.CodeInternal &&
				faults.ReasonOf(result.Err) == "probe_panicked"
		}
	}
	if !foundTimeout || !foundPanic {
		t.Fatalf("timeout=%v panic=%v results=%+v", foundTimeout, foundPanic, report.Results)
	}
	if faults.CodeOf(report.Err()) != faults.CodeInternal {
		t.Fatalf("aggregate code = %s, want internal", faults.CodeOf(report.Err()))
	}
}

func TestProbeSetNilInputs(t *testing.T) {
	t.Parallel()

	if _, err := NewProbeSet(-time.Second); !errors.Is(err, ErrInvalidDuration) {
		t.Fatalf("NewProbeSet error = %v, want ErrInvalidDuration", err)
	} else if faults.CodeOf(err) != faults.CodeInvalidArgument || faults.ReasonOf(err) != "invalid_probe_timeout" {
		t.Fatalf("NewProbeSet classification = %s/%q", faults.CodeOf(err), faults.ReasonOf(err))
	}

	set, err := NewProbeSet(time.Second)
	if err != nil {
		t.Fatalf("NewProbeSet returned %v", err)
	}
	if err := set.Register("nil", nil); !errors.Is(err, ErrNilProbe) {
		t.Fatalf("Register nil error = %v, want ErrNilProbe", err)
	}
	report := set.Check(nil)
	if report.OK || !errors.Is(report.Err(), ErrNilContext) {
		t.Fatalf("nil-context report = %+v, err = %v", report, report.Err())
	}
	if faults.CodeOf(report.Err()) != faults.CodeInvalidArgument {
		t.Fatalf("nil-context code = %s", faults.CodeOf(report.Err()))
	}
}

func TestProbeSetChecksConcurrently(t *testing.T) {
	t.Parallel()

	set, err := NewProbeSet(time.Second)
	if err != nil {
		t.Fatalf("NewProbeSet returned %v", err)
	}

	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	probe := func(context.Context) error {
		entered <- struct{}{}
		<-release
		return nil
	}
	if err := set.Register("one", probe); err != nil {
		t.Fatal(err)
	}
	if err := set.Register("two", probe); err != nil {
		t.Fatal(err)
	}

	done := make(chan ProbeReport, 1)
	go func() { done <- set.Check(context.Background()) }()

	for index := 0; index < 2; index++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("probes did not execute concurrently")
		}
	}
	close(release)

	select {
	case report := <-done:
		if !report.OK {
			t.Fatalf("report = %+v", report)
		}
	case <-time.After(time.Second):
		t.Fatal("probe report did not complete")
	}
}

func TestProbeResultFieldsAreDefensive(t *testing.T) {
	t.Parallel()

	err := faults.New(
		faults.CodeUnavailable,
		"dependency unavailable",
		faults.WithField("dependency", "object-store"),
	)
	result := ProbeResult{
		Name:      "storage",
		OK:        false,
		CheckedAt: time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC),
		Duration:  15 * time.Millisecond,
		Err:       err,
	}
	fields := result.Fields()
	if fields[FieldProbeName] != "storage" || fields["dependency"] != "object-store" || fields["probe_ok"] != false {
		t.Fatalf("unexpected fields: %#v", fields)
	}
	fields[FieldProbeName] = "mutated"
	if result.Fields()[FieldProbeName] != "storage" {
		t.Fatal("ProbeResult.Fields returned shared state")
	}
}

func TestProbeFailuresProviders(t *testing.T) {
	t.Parallel()

	underlying := faults.New(
		faults.CodeUnavailable,
		"registry unavailable",
		faults.WithReason("registry_unavailable"),
		faults.WithOperation("registry.Check"),
		faults.WithField("dependency", "registry"),
		faults.WithRetryPolicy(faults.BackoffRetry(3)),
	)
	failures := &ProbeFailures{Results: []ProbeResult{{Name: "registry", Err: underlying}}}
	if got := failures.Error(); got != "servicekit: probes failed: registry" {
		t.Fatalf("Error() = %q", got)
	}
	if failures.Message() != "registry unavailable" ||
		failures.Reason() != "registry_unavailable" ||
		failures.Operation() != "registry.Check" {
		t.Fatalf("providers = %q/%q/%q", failures.Message(), failures.Reason(), failures.Operation())
	}
	fields := failures.Fields()
	if fields["dependency"] != "registry" || fields["failed_probe_count"] != 1 {
		t.Fatalf("unexpected fields: %#v", fields)
	}
	if policy := failures.RetryPolicy(); policy.Kind != faults.RetryKindBackoff || policy.MaxAttempts != 3 {
		t.Fatalf("retry policy = %+v", policy)
	}

	multiple := &ProbeFailures{Results: []ProbeResult{
		{Name: "a", Err: errors.New("a")},
		{Name: "b", Err: errors.New("b")},
	}}
	if multiple.Message() != "one or more service health checks failed" ||
		multiple.Reason() != "multiple_probes_failed" ||
		multiple.Operation() != operationProbeCheck ||
		multiple.Code() != faults.CodeUnavailable ||
		multiple.RetryPolicy().Specified() {
		t.Fatalf("unexpected multiple providers: %q/%q/%q/%s/%+v",
			multiple.Message(), multiple.Reason(), multiple.Operation(), multiple.Code(), multiple.RetryPolicy())
	}

	empty := &ProbeFailures{}
	if empty.Error() != "servicekit: probes failed" || empty.Code() != faults.CodeUnknown || len(empty.Unwrap()) != 0 {
		t.Fatalf("unexpected empty failures: %q/%s/%v", empty.Error(), empty.Code(), empty.Unwrap())
	}
}

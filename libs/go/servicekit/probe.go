// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package servicekit

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
)

// ProbeResult is the outcome of one liveness or readiness probe.
type ProbeResult struct {
	Name      string
	OK        bool
	CheckedAt time.Time
	Duration  time.Duration
	Err       error
}

// Code returns the result's transport-neutral failure classification.
func (result ProbeResult) Code() faults.Code {
	return faults.CodeOf(result.Err)
}

// Fields returns a fresh set of structured probe attributes.
func (result ProbeResult) Fields() faults.Fields {
	fields := faults.FieldsOf(result.Err).Merge(faults.Fields{
		FieldProbeName: result.Name,
		"probe_ok":     result.OK,
	})
	if !result.CheckedAt.IsZero() {
		fields["checked_at"] = result.CheckedAt.UTC().Format(time.RFC3339Nano)
	}
	if result.Duration > 0 {
		fields["duration_ms"] = result.Duration.Milliseconds()
	}
	return fields.Clone()
}

// ProbeReport is a deterministic aggregate of probe results. Results are
// sorted by probe name.
type ProbeReport struct {
	OK        bool
	CheckedAt time.Time
	Duration  time.Duration
	Results   []ProbeResult
}

// Err returns nil when every probe passed, otherwise a ProbeFailures value.
func (report ProbeReport) Err() error {
	if report.OK {
		return nil
	}
	failures := make([]ProbeResult, 0, len(report.Results))
	for _, result := range report.Results {
		if !result.OK {
			failures = append(failures, result)
		}
	}
	return &ProbeFailures{Results: failures}
}

// ProbeFailures contains the failing results from a ProbeReport.
type ProbeFailures struct {
	Results []ProbeResult
}

var _ error = (*ProbeFailures)(nil)

// Error implements error.
func (failures *ProbeFailures) Error() string {
	if failures == nil || len(failures.Results) == 0 {
		return "servicekit: probes failed"
	}
	return "servicekit: probes failed: " + strings.Join(failures.Names(), ", ")
}

// Unwrap exposes underlying probe errors to errors.Is and errors.As.
func (failures *ProbeFailures) Unwrap() []error {
	if failures == nil {
		return nil
	}
	unwrapped := make([]error, 0, len(failures.Results))
	for _, result := range failures.Results {
		if result.Err != nil {
			unwrapped = append(unwrapped, result.Err)
		}
	}
	return unwrapped
}

// Names returns failing probe names in lexical order.
func (failures *ProbeFailures) Names() []string {
	if failures == nil {
		return nil
	}
	names := make([]string, 0, len(failures.Results))
	for _, result := range failures.Results {
		names = append(names, result.Name)
	}
	sort.Strings(names)
	return names
}

// Code supplies aggregate classification to faults.CodeOf.
func (failures *ProbeFailures) Code() faults.Code {
	if failures == nil || len(failures.Results) == 0 {
		return faults.CodeUnknown
	}
	if len(failures.Results) == 1 {
		if code := failures.Results[0].Code(); code != faults.CodeUnknown {
			return code
		}
	}
	for _, result := range failures.Results {
		switch result.Code() {
		case faults.CodeInternal:
			return faults.CodeInternal
		case faults.CodeDataLoss:
			return faults.CodeDataLoss
		}
	}
	return faults.CodeUnavailable
}

// Message returns a client-safe aggregate summary. A single failure preserves
// its underlying public message.
func (failures *ProbeFailures) Message() string {
	if failures != nil && len(failures.Results) == 1 {
		if message := faults.PublicMessageOf(failures.Results[0].Err); message != "" {
			return message
		}
	}
	return "one or more service health checks failed"
}

// Reason returns stable machine-readable detail. A single failure preserves
// the underlying reason so state and timeout diagnostics are not obscured.
func (failures *ProbeFailures) Reason() string {
	if failures == nil || len(failures.Results) == 0 {
		return "probe_failed"
	}
	if len(failures.Results) == 1 {
		if reason := faults.ReasonOf(failures.Results[0].Err); reason != "" {
			return reason
		}
		return "probe_failed"
	}
	return "multiple_probes_failed"
}

// Operation returns the logical probe operation.
func (failures *ProbeFailures) Operation() string {
	if failures != nil && len(failures.Results) == 1 {
		if operation := faults.OperationOf(failures.Results[0].Err); operation != "" {
			return operation
		}
	}
	return operationProbeCheck
}

// Fields returns aggregate diagnostic metadata. A single failure's fields are
// retained and overlaid with aggregate information.
func (failures *ProbeFailures) Fields() faults.Fields {
	if failures == nil {
		return nil
	}
	fields := faults.Fields(nil)
	if len(failures.Results) == 1 {
		fields = faults.FieldsOf(failures.Results[0].Err)
	}
	return fields.Merge(faults.Fields{
		"failed_probe_count":  len(failures.Results),
		FieldFailedProbeNames: failures.Names(),
	})
}

// RetryPolicy preserves explicit retry intent for a single failing probe.
func (failures *ProbeFailures) RetryPolicy() faults.RetryPolicy {
	if failures == nil || len(failures.Results) != 1 {
		return faults.RetryPolicy{}
	}
	return faults.RetryPolicyOf(failures.Results[0].Err)
}

// ProbeSet is a concurrency-safe registry of named probes.
type ProbeSet struct {
	mu      sync.RWMutex
	timeout time.Duration
	probes  map[string]Probe
	clock   clock.Clock
}

// NewProbeSet creates an independent probe registry. Zero timeout disables the
// package timeout; negative values return a structured invalid-argument fault.
func NewProbeSet(timeout time.Duration) (*ProbeSet, error) {
	if timeout < 0 {
		return nil, structuredFault(
			nil,
			ErrInvalidDuration,
			faults.CodeInvalidArgument,
			"invalid service health probe timeout",
			"invalid_probe_timeout",
			operationProbeSetNew,
			faults.Fields{FieldTimeout: timeout.String()},
		)
	}
	return newProbeSet(timeout, clock.RealClock{}), nil
}

// NewProbeSetWithClock creates an independent probe registry driven by valueClock.
// It is primarily useful for deterministic qualification and simulation.
func NewProbeSetWithClock(timeout time.Duration, valueClock clock.Clock) (*ProbeSet, error) {
	if timeout < 0 {
		return nil, structuredFault(
			nil,
			ErrInvalidDuration,
			faults.CodeInvalidArgument,
			"invalid service health probe timeout",
			"invalid_probe_timeout",
			operationProbeSetNew,
			faults.Fields{FieldTimeout: timeout.String()},
		)
	}
	if nilInterface(valueClock) {
		return nil, structuredFault(nil, ErrNilClock, faults.CodeInvalidArgument, "service clock must not be nil", "nil_clock", operationProbeSetNew, nil)
	}
	return newProbeSet(timeout, valueClock), nil
}

func newProbeSet(timeout time.Duration, valueClock clock.Clock) *ProbeSet {
	if nilInterface(valueClock) {
		valueClock = clock.RealClock{}
	}
	return &ProbeSet{
		timeout: timeout,
		probes:  make(map[string]Probe),
		clock:   valueClock,
	}
}

// Register adds a probe. Probe names are stable diagnostic identifiers.
func (set *ProbeSet) Register(name string, probe Probe) error {
	if set == nil {
		return nilProbeError(name, operationProbeRegister)
	}
	if err := validateName("probe", name, operationProbeRegister); err != nil {
		return err
	}
	if probe == nil {
		return nilProbeError(name, operationProbeRegister)
	}

	set.mu.Lock()
	defer set.mu.Unlock()
	if _, exists := set.probes[name]; exists {
		return duplicateProbeError(name, operationProbeRegister)
	}
	set.probes[name] = probe
	return nil
}

// Replace registers or atomically replaces a probe.
func (set *ProbeSet) Replace(name string, probe Probe) error {
	if set == nil {
		return nilProbeError(name, operationProbeReplace)
	}
	if err := validateName("probe", name, operationProbeReplace); err != nil {
		return err
	}
	if probe == nil {
		return nilProbeError(name, operationProbeReplace)
	}

	set.mu.Lock()
	set.probes[name] = probe
	set.mu.Unlock()
	return nil
}

// Unregister removes a probe and reports whether it existed.
func (set *ProbeSet) Unregister(name string) bool {
	if set == nil {
		return false
	}
	set.mu.Lock()
	defer set.mu.Unlock()
	if _, exists := set.probes[name]; !exists {
		return false
	}
	delete(set.probes, name)
	return true
}

// Names returns registered probe names in lexical order.
func (set *ProbeSet) Names() []string {
	if set == nil {
		return nil
	}
	set.mu.RLock()
	names := make([]string, 0, len(set.probes))
	for name := range set.probes {
		names = append(names, name)
	}
	set.mu.RUnlock()
	sort.Strings(names)
	return names
}

// Check evaluates a snapshot of all probes concurrently.
func (set *ProbeSet) Check(ctx context.Context) ProbeReport {
	if ctx == nil {
		now := time.Now()
		return ProbeReport{
			OK:        false,
			CheckedAt: now,
			Results: []ProbeResult{{
				Name:      "context",
				OK:        false,
				CheckedAt: now,
				Err:       nilContextError(operationProbeCheck),
			}},
		}
	}
	if set == nil {
		now := time.Now()
		return ProbeReport{OK: true, CheckedAt: now}
	}

	startedAt := set.clock.Now()
	probes := set.snapshot()
	if len(probes) == 0 {
		return ProbeReport{OK: true, CheckedAt: startedAt}
	}

	results := make(chan ProbeResult, len(probes))
	var group sync.WaitGroup
	group.Add(len(probes))

	for _, registered := range probes {
		registered := registered
		go func() {
			defer group.Done()
			results <- set.checkOne(ctx, registered.name, registered.probe)
		}()
	}

	group.Wait()
	close(results)

	report := ProbeReport{
		OK:        true,
		CheckedAt: startedAt,
		Results:   make([]ProbeResult, 0, len(probes)),
	}
	for result := range results {
		report.Results = append(report.Results, result)
		report.OK = report.OK && result.OK
	}
	sort.Slice(report.Results, func(left, right int) bool {
		return report.Results[left].Name < report.Results[right].Name
	})
	report.Duration = nonnegativeDuration(set.clock.Now().Sub(startedAt))
	return report
}

type registeredProbe struct {
	name  string
	probe Probe
}

func (set *ProbeSet) snapshot() []registeredProbe {
	set.mu.RLock()
	probes := make([]registeredProbe, 0, len(set.probes))
	for name, probe := range set.probes {
		probes = append(probes, registeredProbe{name: name, probe: probe})
	}
	set.mu.RUnlock()
	sort.Slice(probes, func(left, right int) bool {
		return probes[left].name < probes[right].name
	})
	return probes
}

func (set *ProbeSet) checkOne(ctx context.Context, name string, probe Probe) ProbeResult {
	checkedAt := set.clock.Now()
	probeCtx := faults.ContextWithOperation(ctx, operationProbeCheck)
	cancel := func() {}
	if set.timeout > 0 {
		probeCtx, cancel = withClockTimeout(probeCtx, set.clock, set.timeout)
	}
	defer cancel()

	err := invokeBounded(probeCtx, probe)
	if err != nil {
		err = probeFailure(probeCtx, name, err)
	}

	return ProbeResult{
		Name:      name,
		OK:        err == nil,
		CheckedAt: checkedAt,
		Duration:  nonnegativeDuration(set.clock.Now().Sub(checkedAt)),
		Err:       err,
	}
}

func combineProbeReports(base ProbeResult, report ProbeReport, now func() time.Time) ProbeReport {
	results := make([]ProbeResult, 0, len(report.Results)+1)
	results = append(results, base)
	results = append(results, report.Results...)
	sort.Slice(results, func(left, right int) bool {
		return results[left].Name < results[right].Name
	})

	checkedAt := base.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = now()
	}
	combined := ProbeReport{
		OK:        base.OK && report.OK,
		CheckedAt: checkedAt,
		Results:   results,
	}
	combined.Duration = nonnegativeDuration(now().Sub(checkedAt))
	return combined
}

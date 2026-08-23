// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package local

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/orchestration/launchertest"
	"go.mindclade.dev/libs/go/faults"
)

// helperEnvironment switches the test binary into the child-process role. Using
// the test binary itself keeps the exec path covered without depending on a
// shell or any binary the build sandbox is not guaranteed to have.
const (
	helperEnvironment = "MINDCLADE_LOCAL_HELPER"
	helperModeKey     = "MINDCLADE_LOCAL_HELPER_MODE"
	helperFloodBytes  = 4096
)

// stubCommand is a Command that never touches the operating system.
type stubCommand struct {
	result Result
	err    error
	before func()
}

func (command stubCommand) Run(context.Context) (Result, error) {
	if command.before != nil {
		command.before()
	}
	return command.result, command.err
}

// stubFactory returns the same prepared command for every envelope.
type stubFactory struct {
	command Command
	err     error
}

func (factory stubFactory) NewCommand(context.Context, orchestration.WorkloadEnvelope) (Command, error) {
	if factory.err != nil {
		return nil, factory.err
	}
	return factory.command, nil
}

func TestConformance(t *testing.T) {
	launchertest.Conformance(t, func(tb testing.TB) orchestration.Launcher {
		tb.Helper()
		launcher, err := New(stubFactory{command: stubCommand{result: Result{Stdout: []byte("ok")}}})
		if err != nil {
			tb.Fatalf("New: %v", err)
		}
		return launcher
	})
}

func newLauncher(t *testing.T, factory CommandFactory, options ...Option) *Launcher {
	t.Helper()
	launcher, err := New(factory, options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return launcher
}

func TestNewRejectsMissingFactory(t *testing.T) {
	if _, err := New(nil); !faults.IsCode(err, faults.CodeUnavailable) {
		t.Fatalf("New(nil) = %v, want unavailable", err)
	}
	// A typed nil is not == nil, so the guard has to reflect rather than compare.
	var typed *ExecCommands
	if _, err := New(typed); !faults.IsCode(err, faults.CodeUnavailable) {
		t.Fatalf("New(typed nil) = %v, want unavailable", err)
	}
	if _, err := New(stubFactory{}, WithTrackedWorkloads(0)); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("WithTrackedWorkloads(0) = %v, want invalid_argument", err)
	}
	if _, err := New(stubFactory{}, WithClock(nil)); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("WithClock(nil) = %v, want invalid_argument", err)
	}
}

// TestNonZeroExitIsFailedNotError separates "the workload said no" from "the
// workload could not be run". Collapsing the two would make every failed
// scientific job look like infrastructure trouble to the stage above.
func TestNonZeroExitIsFailedNotError(t *testing.T) {
	launcher := newLauncher(t, stubFactory{command: stubCommand{result: Result{ExitCode: 3, Stderr: []byte("boom")}}})
	envelope := launchertest.Envelope(t, time.Now())
	outcome, err := launcher.Launch(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if outcome.State != orchestration.AttemptFailed {
		t.Fatalf("state = %q, want failed", outcome.State)
	}
	observed, err := launcher.Observe(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if observed.Failure == nil {
		t.Fatal("failed observation carries no failure")
	}
	if orchestration.Classify(observed.Failure) != orchestration.DispositionTerminal {
		t.Fatalf("exit failure classified as %q, want terminal", orchestration.Classify(observed.Failure))
	}
	result, err := launcher.Result(envelope)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if string(result.Stderr) != "boom" || result.ExitCode != 3 {
		t.Fatalf("result = %#v", result)
	}
}

// TestStartFailureIsRetryable is the other half of that split: a process that
// could not be started may start next time, so the fault must schedule a retry
// rather than end the stage.
func TestStartFailureIsRetryable(t *testing.T) {
	launcher := newLauncher(t, stubFactory{err: unavailable("local_process_start_failed", "no", nil)})
	envelope := launchertest.Envelope(t, time.Now())
	if _, err := launcher.Launch(context.Background(), envelope); orchestration.Classify(err) != orchestration.DispositionRetry {
		t.Fatalf("start failure classified as %q, want retry", orchestration.Classify(err))
	}
	observed, err := launcher.Observe(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if observed.State != orchestration.AttemptFailed {
		t.Fatalf("state = %q, want failed", observed.State)
	}
	// The record survives the failure, so the next duplicate delivery is still
	// idempotent rather than starting the work a second time.
	repeat, err := launcher.Launch(context.Background(), envelope)
	if err != nil {
		t.Fatalf("redelivered launch: %v", err)
	}
	if !repeat.Existed {
		t.Fatal("redelivered launch after a failure did not report the workload existed")
	}
}

// TestCancelDuringRunStopsTheAttempt exercises the window this launcher has to
// get right despite waiting inline: Launch releases the registry lock across
// the run, so a cancellation can land while the process is executing.
func TestCancelDuringRunStopsTheAttempt(t *testing.T) {
	var launcher *Launcher
	envelope := launchertest.Envelope(t, time.Now())
	launcher = newLauncher(t, stubFactory{command: stubCommand{
		result: Result{Stdout: []byte("late")},
		before: func() {
			if err := launcher.Cancel(context.Background(), envelope, "operator stop"); err != nil {
				t.Errorf("cancel during run: %v", err)
			}
		},
	}})
	outcome, err := launcher.Launch(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if outcome.State != orchestration.AttemptCancelled {
		t.Fatalf("state = %q, want cancelled", outcome.State)
	}
	observed, err := launcher.Observe(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if observed.State != orchestration.AttemptCancelled || observed.Failure == nil {
		t.Fatalf("observation = %#v, want a cancelled state carrying its cause", observed)
	}
}

// TestNewerFenceSupersedes is the mirror of the conformance stale-fence case.
// Recovery mints a higher fence, and the higher fence has to win or a recovered
// attempt could never be launched at all.
func TestNewerFenceSupersedes(t *testing.T) {
	launcher := newLauncher(t, stubFactory{command: stubCommand{}})
	envelope := launchertest.Envelope(t, time.Now())
	first, err := launcher.Launch(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	newer := launchertest.Refence(envelope, launchertest.DefaultFencingToken+1)
	newer.Attempt = 2
	newer.ExecutionTicket.Claims.Attempt = 2
	second, err := launcher.Launch(context.Background(), newer)
	if err != nil {
		t.Fatalf("superseding launch: %v", err)
	}
	if second.Existed {
		t.Fatal("superseding launch reported the workload already existed")
	}
	if second.ExternalID == first.ExternalID {
		t.Fatal("superseding launch reused the replaced attempt's external id")
	}
	// The replaced attempt is gone, so the old owner cannot address it.
	if _, err := launcher.Observe(context.Background(), envelope); !faults.IsCode(err, faults.CodeConflict) {
		t.Fatalf("observe under the replaced fence = %v, want conflict", err)
	}
}

func TestTrackedWorkloadBoundIsRefusedNotEvicted(t *testing.T) {
	launcher := newLauncher(t, stubFactory{command: stubCommand{}}, WithTrackedWorkloads(1))
	first := launchertest.Envelope(t, time.Now())
	if _, err := launcher.Launch(context.Background(), first); err != nil {
		t.Fatalf("first launch: %v", err)
	}
	second := launchertest.Envelope(t, time.Now())
	_, err := launcher.Launch(context.Background(), second)
	if !faults.IsCode(err, faults.CodeResourceExhausted) {
		t.Fatalf("launch beyond the bound = %v, want resource_exhausted", err)
	}
	// Backpressure must not charge the stage an attempt.
	if orchestration.Classify(err) != orchestration.DispositionReschedule {
		t.Fatalf("bound refusal classified as %q, want reschedule", orchestration.Classify(err))
	}
	// The first workload is still addressable, which is what "refused, not
	// evicted" means.
	if _, err := launcher.Observe(context.Background(), first); err != nil {
		t.Fatalf("observe the retained workload: %v", err)
	}
}

func TestExternalIDIsDeterministicAndDistinct(t *testing.T) {
	envelope := launchertest.Envelope(t, time.Now())
	first := ExternalID(envelope)
	second := ExternalID(envelope)
	if first != second {
		t.Fatal("external id is not deterministic")
	}
	if !strings.HasPrefix(first, ExternalIDPrefix) {
		t.Fatalf("external id %q lacks the launcher prefix", first)
	}
	// The attempt is part of the identity: a retry is a different workload with
	// a different handle, even though everything else about it matches.
	retry := envelope
	retry.Attempt = 2
	if ExternalID(retry) == first {
		t.Fatal("two attempts share one external id")
	}
	if err := (orchestration.LaunchOutcome{ExternalID: first, State: orchestration.AttemptCreated}).Validate(); err != nil {
		t.Fatalf("external id is not a valid launch handle: %v", err)
	}
}

func TestBoundedWriterTruncatesInsteadOfFailing(t *testing.T) {
	writer := &boundedWriter{limit: 4}
	count, err := writer.Write([]byte("abcdef"))
	if err != nil || count != 6 {
		t.Fatalf("Write = %d, %v, want 6, nil", count, err)
	}
	if string(writer.bytes()) != "abcd" || !writer.truncated {
		t.Fatalf("buffer = %q truncated=%v", writer.bytes(), writer.truncated)
	}
	// A short write here would make io.Copy inside os/exec kill the workload
	// for being verbose, so a full-buffer write must still claim success.
	if count, err := writer.Write([]byte("gh")); err != nil || count != 2 {
		t.Fatalf("Write after the bound = %d, %v, want 2, nil", count, err)
	}
	if string(writer.bytes()) != "abcd" {
		t.Fatalf("buffer grew past the bound: %q", writer.bytes())
	}
	empty := &boundedWriter{limit: 4}
	if empty.bytes() != nil {
		t.Fatal("an unwritten bounded writer returned a non-nil buffer")
	}
}

func TestExecCommandsRejectsUnusableProgram(t *testing.T) {
	if _, err := (ExecCommands{}).NewCommand(context.Background(), orchestration.WorkloadEnvelope{}); !faults.IsCode(err, faults.CodeUnavailable) {
		t.Fatalf("missing resolver = %v, want unavailable", err)
	}
	var missing context.Context
	if _, err := (ExecCommands{Resolver: fixedResolver{}}).NewCommand(missing, orchestration.WorkloadEnvelope{}); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("nil context = %v, want invalid_argument", err)
	}
	if _, err := (ExecCommands{Resolver: fixedResolver{}}).NewCommand(context.Background(), orchestration.WorkloadEnvelope{}); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("empty program path = %v, want invalid_argument", err)
	}
	bad := fixedResolver{program: Program{Path: "/bin/true", Env: []string{"NOT_A_PAIR"}}}
	if _, err := (ExecCommands{Resolver: bad}).NewCommand(context.Background(), orchestration.WorkloadEnvelope{}); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("malformed environment = %v, want invalid_argument", err)
	}
}

type fixedResolver struct {
	program Program
}

func (resolver fixedResolver) Resolve(orchestration.WorkloadEnvelope) (Program, error) {
	return resolver.program, nil
}

// TestExecCommandsRunsHelperProcess proves the real exec path: a started
// process, a blocking Wait, a captured exit code, and bounded output.
func TestExecCommandsRunsHelperProcess(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Skipf("test binary path is unavailable: %v", err)
	}
	factory := ExecCommands{
		Resolver: fixedResolver{program: Program{
			Path: binary,
			Args: []string{"-test.run=^TestHelperProcess$", "-test.v=false"},
			Env:  []string{helperEnvironment + "=1", helperModeKey + "=flood"},
		}},
		CaptureLimit: 64,
	}
	command, err := factory.NewCommand(context.Background(), orchestration.WorkloadEnvelope{})
	if err != nil {
		t.Fatalf("NewCommand: %v", err)
	}
	result, err := command.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", result.ExitCode)
	}
	if len(result.Stdout) > 64 || len(result.Stderr) > 64 {
		t.Fatalf("capture exceeded its bound: %d stdout, %d stderr", len(result.Stdout), len(result.Stderr))
	}
	if !result.Truncated {
		t.Fatal("a flooded capture did not report truncation")
	}
}

func TestExecCommandsRefusesADoneContext(t *testing.T) {
	command := &execCommand{program: Program{Path: "/nonexistent"}, limit: 8, waitDelay: time.Second}
	var missing context.Context
	if _, err := command.Run(missing); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("nil context = %v, want invalid_argument", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := command.Run(ctx); !faults.IsCode(err, faults.CodeCanceled) {
		t.Fatalf("cancelled context = %v, want canceled", err)
	}
}

// TestHelperProcess is the child half of TestExecCommandsRunsHelperProcess. It
// is inert unless the launcher started it.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnvironment) != "1" {
		t.Skip("not running as the helper process")
	}
	if os.Getenv(helperModeKey) != "flood" {
		t.Fatalf("unknown helper mode %q", os.Getenv(helperModeKey))
	}
	payload := strings.Repeat("x", helperFloodBytes)
	if _, err := os.Stdout.WriteString(payload); err != nil {
		t.Fatalf("write stdout: %v", err)
	}
	if _, err := os.Stderr.WriteString(payload); err != nil {
		t.Fatalf("write stderr: %v", err)
	}
	os.Exit(7)
}

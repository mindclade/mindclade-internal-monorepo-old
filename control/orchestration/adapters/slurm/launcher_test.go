// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package slurm

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/orchestration/launchertest"
	"go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/libs/go/faults"
)

// fakeClient is an in-memory Slurm controller. It is enough to drive the whole
// launcher contract, which is the point of keeping the provider surface to
// three verbs.
type fakeClient struct {
	jobs     map[string]*Job
	submits  int
	cancels  int
	failNext error
	rename   string
}

func newFakeClient() *fakeClient { return &fakeClient{jobs: map[string]*Job{}} }

func (client *fakeClient) Submit(_ context.Context, request SubmitRequest) (Job, error) {
	if client.failNext != nil {
		err := client.failNext
		client.failNext = nil
		return Job{}, err
	}
	client.submits++
	name := request.Reference.Name
	if client.rename != "" {
		name = client.rename
	}
	job := Job{
		Reference: Reference{Name: name, ID: "job-1"},
		State:     StatePending,
		Fence:     request.Fence,
		Revision:  1,
	}
	client.jobs[request.Reference.Name] = &job
	return job, nil
}

func (client *fakeClient) Query(_ context.Context, reference Reference) (Job, error) {
	job, found := client.jobs[reference.Name]
	if !found {
		return Job{}, faults.New(faults.CodeNotFound, "no such slurm job",
			faults.WithReason("job_not_found"), faults.WithOperation("test"),
			faults.WithRetryPolicy(faults.NoRetry()))
	}
	return *job, nil
}

func (client *fakeClient) Cancel(_ context.Context, reference Reference, _ string) error {
	client.cancels++
	job, found := client.jobs[reference.Name]
	if !found {
		return nil
	}
	job.State = StateCancelled
	job.Revision++
	return nil
}

type fakeResolver struct {
	submission Submission
	err        error
}

func (resolver fakeResolver) Resolve(orchestration.WorkloadEnvelope) (Submission, error) {
	if resolver.err != nil {
		return Submission{}, resolver.err
	}
	return resolver.submission, nil
}

func validSubmission() Submission {
	return Submission{Partition: "batch", Account: "mindclade", Nodes: 1, Script: "#!/bin/sh\nexit 0\n"}
}

func TestConformance(t *testing.T) {
	launchertest.Conformance(t, func(tb testing.TB) orchestration.Launcher {
		tb.Helper()
		launcher, err := New(newFakeClient(), fakeResolver{submission: validSubmission()})
		if err != nil {
			tb.Fatalf("New: %v", err)
		}
		return launcher
	})
}

func newLauncher(t *testing.T, client Client, resolver Resolver) *Launcher {
	t.Helper()
	launcher, err := New(client, resolver)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return launcher
}

func TestNewRejectsMissingCollaborators(t *testing.T) {
	if _, err := New(nil, fakeResolver{}); !faults.IsCode(err, faults.CodeUnavailable) {
		t.Fatalf("New(nil client) = %v, want unavailable", err)
	}
	if _, err := New(newFakeClient(), nil); !faults.IsCode(err, faults.CodeUnavailable) {
		t.Fatalf("New(nil resolver) = %v, want unavailable", err)
	}
	var typed *fakeClient
	if _, err := New(typed, fakeResolver{}); !faults.IsCode(err, faults.CodeUnavailable) {
		t.Fatalf("New(typed nil client) = %v, want unavailable", err)
	}
}

// TestLaunchQueriesBeforeSubmitting is the property that makes an at-least-once
// queue safe on a scheduler with no idempotency key.
func TestLaunchQueriesBeforeSubmitting(t *testing.T) {
	client := newFakeClient()
	launcher := newLauncher(t, client, fakeResolver{submission: validSubmission()})
	envelope := launchertest.Envelope(t, time.Now())
	for range 4 {
		if _, err := launcher.Launch(context.Background(), envelope); err != nil {
			t.Fatalf("Launch: %v", err)
		}
	}
	if client.submits != 1 {
		t.Fatalf("submits = %d, want 1", client.submits)
	}
}

// TestCancelOfAFinishedJobDoesNotReachTheProvider keeps a cancellation that
// raced a completion from asking Slurm to kill a job that already published its
// outcome.
func TestCancelOfAFinishedJobDoesNotReachTheProvider(t *testing.T) {
	client := newFakeClient()
	launcher := newLauncher(t, client, fakeResolver{submission: validSubmission()})
	envelope := launchertest.Envelope(t, time.Now())
	if _, err := launcher.Launch(context.Background(), envelope); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	job := client.jobs[JobName(envelope)]
	job.State = StateCompleted
	job.Revision++
	if err := launcher.Cancel(context.Background(), envelope, "too late"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if client.cancels != 0 {
		t.Fatalf("cancels = %d, want 0", client.cancels)
	}
	observed, err := launcher.Observe(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if observed.State != orchestration.AttemptCompleted {
		t.Fatalf("state = %q, want completed", observed.State)
	}
}

// TestProviderRenameIsRefused catches an adapter or provider that lost the
// deterministic name, which would silently disable idempotence rather than fail.
func TestProviderRenameIsRefused(t *testing.T) {
	client := newFakeClient()
	client.rename = "mc-somethingelse"
	launcher := newLauncher(t, client, fakeResolver{submission: validSubmission()})
	envelope := launchertest.Envelope(t, time.Now())
	if _, err := launcher.Launch(context.Background(), envelope); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("renamed submission = %v, want failed_precondition", err)
	}
}

func TestJobNameAndCommentCarryAttemptIdentity(t *testing.T) {
	envelope := launchertest.Envelope(t, time.Now())
	first := JobName(envelope)
	second := JobName(envelope)
	if first != second {
		t.Fatal("job name is not deterministic")
	}
	if !strings.HasPrefix(first, JobNamePrefix) {
		t.Fatalf("job name %q lacks the ownership prefix", first)
	}
	retry := envelope
	retry.Attempt = 2
	if JobName(retry) == first {
		t.Fatal("two attempts share one job name")
	}
	comment := Comment(envelope)
	for _, want := range []string{CommentPrefix, "workload=" + envelope.WorkloadID, "fence=8"} {
		if !strings.Contains(comment, want) {
			t.Fatalf("comment %q is missing %q", comment, want)
		}
	}
	if strings.ContainsAny(comment, " \t\n") {
		t.Fatalf("comment %q contains whitespace slurm would mangle", comment)
	}
}

// TestProjectCoversEveryMappedState pins the terminality of the whole table.
// Getting one of these wrong either strands a stage forever or declares a
// running job dead, and neither failure is visible from the launcher's own API.
func TestProjectCoversEveryMappedState(t *testing.T) {
	cases := map[State]orchestration.AttemptState{
		StatePending:     orchestration.AttemptCreated,
		StateConfiguring: orchestration.AttemptStarting,
		StateRequeued:    orchestration.AttemptRecovering,
		StateResizing:    orchestration.AttemptRecovering,
		StateRunning:     orchestration.AttemptRunning,
		StateSuspended:   orchestration.AttemptDraining,
		StateStopped:     orchestration.AttemptDraining,
		StateSignaling:   orchestration.AttemptCancelling,
		StateStageOut:    orchestration.AttemptCommitting,
		StateCompleting:  orchestration.AttemptCommitting,
		StateCompleted:   orchestration.AttemptCompleted,
		StateCancelled:   orchestration.AttemptCancelled,
		StateRevoked:     orchestration.AttemptCancelled,
		StateFailed:      orchestration.AttemptFailed,
		StateSpecialExit: orchestration.AttemptFailed,
		StateTimeout:     orchestration.AttemptFailed,
		StateDeadline:    orchestration.AttemptFailed,
		StateNodeFail:    orchestration.AttemptFailed,
		StateBootFail:    orchestration.AttemptFailed,
		StateOutOfMemory: orchestration.AttemptFailed,
		StatePreempted:   orchestration.AttemptFailed,
	}
	for slurmState, want := range cases {
		projected, err := Project(Job{State: slurmState, Revision: 1})
		if err != nil {
			t.Fatalf("Project(%q): %v", slurmState, err)
		}
		if projected.State != want {
			t.Fatalf("Project(%q) = %q, want %q", slurmState, projected.State, want)
		}
		if !projected.State.Valid() {
			t.Fatalf("Project(%q) produced an invalid attempt state", slurmState)
		}
		unsuccessful := projected.State == orchestration.AttemptFailed || projected.State == orchestration.AttemptCancelled
		if unsuccessful != (projected.Failure != nil) {
			t.Fatalf("Project(%q) failure presence = %v for state %q", slurmState, projected.Failure != nil, projected.State)
		}
	}
	// Case is normalised, so a provider reporting lower-case states is mapped
	// rather than refused.
	if projected, err := Project(Job{State: "running", Revision: 1}); err != nil || projected.State != orchestration.AttemptRunning {
		t.Fatalf("Project(\"running\") = %#v, %v", projected, err)
	}
	if _, err := Project(Job{State: "SOMETHING_NEW", Revision: 1}); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("unmapped state = %v, want failed_precondition", err)
	}
}

// TestPreemptionIsReschedulableAndFailureIsNot is the whole reason Failure
// carries a fault rather than a string: the disposition is read off the code.
func TestPreemptionIsReschedulableAndFailureIsNot(t *testing.T) {
	preempted, err := Project(Job{State: StatePreempted, Revision: 1, Reason: "Preempted"})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if orchestration.Classify(preempted.Failure) != orchestration.DispositionReschedule {
		t.Fatalf("preemption classified as %q, want reschedule", orchestration.Classify(preempted.Failure))
	}
	failed, err := Project(Job{State: StateFailed, Revision: 1, ExitCode: 2})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if orchestration.Classify(failed.Failure) != orchestration.DispositionTerminal {
		t.Fatalf("failure classified as %q, want terminal", orchestration.Classify(failed.Failure))
	}
}

// TestSequenceOrdersARequeue is the case a rank-only sequence gets wrong: the
// job goes back to PENDING, and the newer observation must still sort later.
func TestSequenceOrdersARequeue(t *testing.T) {
	running, err := Sequence(Job{State: StateRunning, Revision: 4})
	if err != nil {
		t.Fatalf("Sequence: %v", err)
	}
	requeued, err := Sequence(Job{State: StatePending, Revision: 5})
	if err != nil {
		t.Fatalf("Sequence: %v", err)
	}
	if requeued <= running {
		t.Fatalf("requeued sequence %d does not follow running sequence %d", requeued, running)
	}
	if _, err := Sequence(Job{State: StateRunning}); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("missing revision = %v, want failed_precondition", err)
	}
	if _, err := Sequence(Job{State: StateRunning, Revision: MaximumRevision + 1}); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("oversized revision = %v, want failed_precondition", err)
	}
}

func TestWholeCoresRoundsUp(t *testing.T) {
	for millis, want := range map[uint32]uint32{1: 1, 999: 1, 1000: 1, 1001: 2, 2500: 3, 8000: 8} {
		if got := wholeCores(millis); got != want {
			t.Fatalf("wholeCores(%d) = %d, want %d", millis, got, want)
		}
	}
}

func TestEphemeralBytesSumsAndRefusesZero(t *testing.T) {
	total, err := ephemeralBytes(runtime_authority.ExecutionBudget{
		LocalDiskBytes:         1 << 20,
		CheckpointStagingBytes: 1 << 21,
		TelemetrySpoolBytes:    1 << 22,
	})
	if err != nil {
		t.Fatalf("ephemeralBytes: %v", err)
	}
	if total != (1<<20)+(1<<21)+(1<<22) {
		t.Fatalf("total = %d", total)
	}
	if _, err := ephemeralBytes(runtime_authority.ExecutionBudget{}); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("empty disk budget = %v, want invalid_argument", err)
	}
	overflow := runtime_authority.ExecutionBudget{LocalDiskBytes: mathMaxUint64, CheckpointStagingBytes: 1}
	if _, err := ephemeralBytes(overflow); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("overflowing disk budget = %v, want invalid_argument", err)
	}
}

func TestSubmissionValidationMatchesTheTicket(t *testing.T) {
	envelope := launchertest.Envelope(t, time.Now())
	if err := validateSubmission(validSubmission(), envelope); err != nil {
		t.Fatalf("valid submission: %v", err)
	}
	for name, mutate := range map[string]func(*Submission){
		"partition":   func(s *Submission) { s.Partition = " " },
		"script":      func(s *Submission) { s.Script = "" },
		"nodes":       func(s *Submission) { s.Nodes = 0 },
		"environment": func(s *Submission) { s.Environment = []string{"BAREWORD"} },
	} {
		submission := validSubmission()
		mutate(&submission)
		if err := validateSubmission(submission, envelope); !faults.IsCode(err, faults.CodeInvalidArgument) {
			t.Fatalf("%s = %v, want invalid_argument", name, err)
		}
	}
	// Devices without a GPU budget is capacity nobody authorised.
	greedy := validSubmission()
	greedy.GPUs = 8
	if err := validateSubmission(greedy, envelope); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("unbudgeted devices = %v, want failed_precondition", err)
	}
	// A GPU budget without devices is a job that will never be placed.
	accelerated := envelope
	accelerated.ExecutionTicket.Claims.Budget.GPUMemoryEstimateBytes = 1 << 30
	if err := validateSubmission(validSubmission(), accelerated); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("budgeted gpu with no devices = %v, want failed_precondition", err)
	}
	greedy.GPUs = 8
	if err := validateSubmission(greedy, accelerated); err != nil {
		t.Fatalf("matched gpu request: %v", err)
	}
}

func TestResolverFailureStopsTheSubmission(t *testing.T) {
	client := newFakeClient()
	launcher := newLauncher(t, client, fakeResolver{err: invalid("policy_missing", "no submission policy", nil)})
	if _, err := launcher.Launch(context.Background(), launchertest.Envelope(t, time.Now())); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("resolver failure = %v, want invalid_argument", err)
	}
	if client.submits != 0 {
		t.Fatalf("submits = %d, want 0", client.submits)
	}
}

func TestProviderFailurePropagates(t *testing.T) {
	client := newFakeClient()
	client.failNext = unavailable("provider_down", "slurmctld is unreachable", nil)
	launcher := newLauncher(t, client, fakeResolver{submission: validSubmission()})
	_, err := launcher.Launch(context.Background(), launchertest.Envelope(t, time.Now()))
	if !faults.IsCode(err, faults.CodeUnavailable) {
		t.Fatalf("provider failure = %v, want unavailable", err)
	}
	if orchestration.Classify(err) != orchestration.DispositionRetry {
		t.Fatalf("provider failure classified as %q, want retry", orchestration.Classify(err))
	}
}

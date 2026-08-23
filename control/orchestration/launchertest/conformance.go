// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package launchertest

import (
	"context"
	"testing"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

// DefaultFencingToken is the fence every fixture envelope carries. It is
// deliberately above one so a test can mint a strictly older fence without
// reaching zero, which the ticket contract rejects outright and which would
// therefore fail for the wrong reason.
const DefaultFencingToken = uint64(8)

// Factory builds the launcher under test. It is called once per conformance
// case so no case can observe state another case left behind.
//
// The launcher it returns must read wall time, because the suite mints
// envelopes whose ticket window is anchored to time.Now(). A launcher wired to
// a fake clock at some unrelated instant would reject every fixture as expired
// and report a contract failure that is really a test-wiring failure.
type Factory func(testing.TB) orchestration.Launcher

// Conformance runs the six qualification cases every adapter README names.
func Conformance(t *testing.T, factory Factory) {
	t.Helper()
	if factory == nil {
		t.Fatal("launchertest.Conformance requires a factory")
	}
	t.Run("IdempotentLaunch", func(t *testing.T) { idempotentLaunch(t, factory) })
	t.Run("StatusReconciliation", func(t *testing.T) { statusReconciliation(t, factory) })
	t.Run("Cancellation", func(t *testing.T) { cancellation(t, factory) })
	t.Run("DuplicateDelivery", func(t *testing.T) { duplicateDelivery(t, factory) })
	t.Run("Timeout", func(t *testing.T) { timeout(t, factory) })
	t.Run("StaleFence", func(t *testing.T) { staleFence(t, factory) })
}

// Envelope mints a valid envelope whose ticket window brackets now.
//
// Every identifier is freshly minted, so two envelopes never collide and a
// launcher that keyed its registry on something coarser than workload identity
// would fail the idempotence case rather than quietly passing it.
func Envelope(tb testing.TB, now time.Time) orchestration.WorkloadEnvelope {
	tb.Helper()
	digest := identifiers.SHA256String("launchertest-resolved-config")
	run := newID(tb, "run")
	job := newID(tb, "job")
	stage := newID(tb, "stage")
	tenant := newID(tb, "tenant")
	workspace := newID(tb, "workspace")
	envelope := orchestration.WorkloadEnvelope{
		WorkloadID:  newID(tb, "workload"),
		RunID:       run,
		JobID:       job,
		StageID:     stage,
		Attempt:     1,
		TenantID:    tenant,
		WorkspaceID: workspace,
		ExecutionTicket: runtime_authority.ExecutionTicket{
			Claims: runtime_authority.ExecutionTicketClaims{
				TicketID:             newID(tb, "ticket"),
				Issuer:               "launchertest",
				TenantID:             tenant,
				WorkspaceID:          workspace,
				RunID:                run,
				JobID:                job,
				StageID:              stage,
				Attempt:              1,
				FencingToken:         DefaultFencingToken,
				ResolvedConfigDigest: digest,
				ExecutionClass:       "batch-cpu",
				NotBefore:            now.Add(-time.Minute),
				Deadline:             now.Add(time.Hour),
				Expires:              now.Add(30 * time.Minute),
				PolicyEpoch:          1,
				RouteSnapshotVersion: 1,
				RevocationEpoch:      1,
				Budget: runtime_authority.ExecutionBudget{
					CPUMillis:           1000,
					ResidentMemoryBytes: 1 << 30,
					LocalDiskBytes:      1 << 30,
					OpenFileDescriptors: 1024,
					CPUWorkerThreads:    4,
				},
			},
		},
		ResolvedConfigDigest: digest,
		ResourceClass:        "batch-cpu",
		CreatedAt:            now.Add(-time.Minute),
		Deadline:             now.Add(time.Hour),
		StageKind:            orchestration.StageBatchInference,
		Operation:            "launchertest.execute",
	}
	if err := envelope.Validate(now); err != nil {
		tb.Fatalf("launchertest fixture envelope is not valid at %s: %v", now, err)
	}
	return envelope
}

// Refence returns a copy of envelope carrying a different fencing token. The
// ticket signature is not recomputed because no launcher verifies it: fence
// authority is checked by whoever issued the ticket, and a launcher that
// re-derived it would be duplicating runtime_authority badly.
func Refence(envelope orchestration.WorkloadEnvelope, fence uint64) orchestration.WorkloadEnvelope {
	envelope.ExecutionTicket.Claims.FencingToken = fence
	return envelope
}

func newID(tb testing.TB, kind string) string {
	tb.Helper()
	identifier, err := identifiers.NewID(identifiers.MustParseKind(kind))
	if err != nil {
		tb.Fatalf("mint %s identifier: %v", kind, err)
	}
	return identifier.String()
}

// idempotentLaunch is the property that makes an at-least-once queue safe: the
// second delivery of one work item must address the workload the first one
// started, not start a second copy of it.
func idempotentLaunch(t *testing.T, factory Factory) {
	t.Helper()
	launcher := factory(t)
	ctx := context.Background()
	envelope := Envelope(t, time.Now())

	first, err := launcher.Launch(ctx, envelope)
	if err != nil {
		t.Fatalf("first launch: %v", err)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("first launch outcome is not valid: %v", err)
	}
	if first.Existed {
		t.Fatal("first launch reported the workload already existed")
	}

	second, err := launcher.Launch(ctx, envelope)
	if err != nil {
		t.Fatalf("second launch: %v", err)
	}
	if err := second.Validate(); err != nil {
		t.Fatalf("second launch outcome is not valid: %v", err)
	}
	if !second.Existed {
		t.Fatal("second launch did not report the workload already existed")
	}
	if second.ExternalID != first.ExternalID {
		t.Fatalf("second launch minted a new external id: %q != %q", second.ExternalID, first.ExternalID)
	}

	// A third delivery must be indistinguishable from the second. A launcher
	// that only special-cased the first duplicate would pass the check above
	// and still create a copy on the next redelivery.
	third, err := launcher.Launch(ctx, envelope)
	if err != nil {
		t.Fatalf("third launch: %v", err)
	}
	if !third.Existed || third.ExternalID != first.ExternalID {
		t.Fatalf("third launch = %#v, want existing %q", third, first.ExternalID)
	}
}

// statusReconciliation checks that Observe is usable as the input to a state
// machine: it names the same object Launch did, it carries an orderable
// sequence, and it never walks a terminal attempt backwards.
func statusReconciliation(t *testing.T, factory Factory) {
	t.Helper()
	launcher := factory(t)
	ctx := context.Background()
	envelope := Envelope(t, time.Now())

	// Observing something that was never launched is a missing object, not an
	// invented "created" state. Reporting a state here would let a reconciler
	// conclude an attempt is in flight when nothing was ever started.
	if _, err := launcher.Observe(ctx, envelope); !faults.IsCode(err, faults.CodeNotFound) {
		t.Fatalf("observe before launch = %v, want not_found", err)
	}

	outcome, err := launcher.Launch(ctx, envelope)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	track := tracker{t: t, externalID: outcome.ExternalID}
	for range 3 {
		track.record(launcher.Observe(ctx, envelope))
	}
	if track.count == 0 {
		t.Fatal("no observation was recorded")
	}
}

// cancellation covers the two halves of the contract that are easy to get
// backwards: cancelling something that is not there must succeed, and
// cancelling something already finished must not rewrite its outcome.
func cancellation(t *testing.T, factory Factory) {
	t.Helper()
	launcher := factory(t)
	ctx := context.Background()
	const reason = "conformance cancellation"

	// Nothing launched. A cancellation that raced the launcher losing its
	// claim arrives exactly like this, and failing it would turn a benign race
	// into a stuck stage.
	absent := Envelope(t, time.Now())
	if err := launcher.Cancel(ctx, absent, reason); err != nil {
		t.Fatalf("cancel of an unlaunched workload: %v", err)
	}

	envelope := Envelope(t, time.Now())
	outcome, err := launcher.Launch(ctx, envelope)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	before := observation(t, ctx, launcher, envelope)

	if err := launcher.Cancel(ctx, envelope, reason); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// Idempotent, because the cancellation intent is durable and will be
	// replayed until the stage records an outcome.
	if err := launcher.Cancel(ctx, envelope, reason); err != nil {
		t.Fatalf("second cancel: %v", err)
	}

	after := observation(t, ctx, launcher, envelope)
	if after.ExternalID != outcome.ExternalID {
		t.Fatalf("cancel changed the external id: %q != %q", after.ExternalID, outcome.ExternalID)
	}
	if before.State.Terminal() && after.State != before.State {
		t.Fatalf("cancel rewrote a terminal outcome: %q -> %q", before.State, after.State)
	}
	if !before.State.Terminal() && after.State == before.State {
		t.Fatalf("cancel left a live attempt in %q", after.State)
	}
	if after.Sequence < before.Sequence {
		t.Fatalf("observation sequence regressed across cancel: %d < %d", after.Sequence, before.Sequence)
	}

	// An unbounded or empty reason is refused. The reason is written to a
	// durable cancellation record and, on Kubernetes, to an object annotation,
	// so "no reason" is a missing audit trail rather than a default.
	if err := launcher.Cancel(ctx, envelope, ""); err == nil {
		t.Fatal("cancel accepted an empty reason")
	} else if orchestration.Classify(err) != orchestration.DispositionTerminal {
		t.Fatalf("empty cancel reason classified as %q, want terminal", orchestration.Classify(err))
	}
}

// duplicateDelivery is idempotentLaunch after the attempt has been stopped. It
// is the case that separates a launcher which remembers outcomes from one that
// merely deduplicates while an object happens to exist.
func duplicateDelivery(t *testing.T, factory Factory) {
	t.Helper()
	launcher := factory(t)
	ctx := context.Background()
	envelope := Envelope(t, time.Now())

	first, err := launcher.Launch(ctx, envelope)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if err := launcher.Cancel(ctx, envelope, "conformance duplicate delivery"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	stopped := observation(t, ctx, launcher, envelope)

	// The queue redelivers the same work item after the attempt was stopped.
	// Restarting here would run a workload no live ticket authorizes.
	redelivered, err := launcher.Launch(ctx, envelope)
	if err != nil {
		t.Fatalf("redelivered launch: %v", err)
	}
	if !redelivered.Existed {
		t.Fatal("redelivered launch did not report the workload already existed")
	}
	if redelivered.ExternalID != first.ExternalID {
		t.Fatalf("redelivered launch minted a new external id: %q != %q", redelivered.ExternalID, first.ExternalID)
	}
	if stopped.State.Terminal() && redelivered.State != stopped.State {
		t.Fatalf("redelivered launch restarted a terminal attempt: %q -> %q", stopped.State, redelivered.State)
	}
}

// timeout covers the two ways a request can be out of time: the envelope's own
// ticket window has closed, or the caller's context is already done.
func timeout(t *testing.T, factory Factory) {
	t.Helper()
	launcher := factory(t)
	ctx := context.Background()

	// Deadline and ticket expiry are two hours behind wall time.
	expired := Envelope(t, time.Now().Add(-2*time.Hour))
	if _, err := launcher.Launch(ctx, expired); err == nil {
		t.Fatal("launch accepted an expired envelope")
	} else if orchestration.Classify(err) != orchestration.DispositionTerminal {
		t.Fatalf("expired launch classified as %q, want terminal", orchestration.Classify(err))
	}
	if _, err := launcher.Observe(ctx, expired); err == nil {
		t.Fatal("observe accepted an expired envelope")
	}

	done, cancel := context.WithCancel(context.Background())
	cancel()
	envelope := Envelope(t, time.Now())
	if _, err := launcher.Launch(done, envelope); err == nil {
		t.Fatal("launch accepted a cancelled context")
	}
	if _, err := launcher.Observe(done, envelope); err == nil {
		t.Fatal("observe accepted a cancelled context")
	}
	if err := launcher.Cancel(done, envelope, "conformance timeout"); err == nil {
		t.Fatal("cancel accepted a cancelled context")
	}
	// The refused launch must not have left anything behind.
	if _, err := launcher.Observe(ctx, envelope); !faults.IsCode(err, faults.CodeNotFound) {
		t.Fatalf("observe after a refused launch = %v, want not_found", err)
	}
}

// staleFence is the fencing property, stated for launchers. A replaced replica
// still holding an old ticket must not be able to address the workload its
// successor now owns, and it must be told so with a fault that ends the
// attempt rather than one that schedules a retry.
func staleFence(t *testing.T, factory Factory) {
	t.Helper()
	launcher := factory(t)
	ctx := context.Background()
	envelope := Envelope(t, time.Now())

	current, err := launcher.Launch(ctx, envelope)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	stale := Refence(envelope, DefaultFencingToken-1)
	if _, err := launcher.Launch(ctx, stale); err == nil {
		t.Fatal("launch accepted a stale fencing token")
	} else if orchestration.Classify(err) != orchestration.DispositionTerminal {
		t.Fatalf("stale launch classified as %q, want terminal", orchestration.Classify(err))
	}
	if err := launcher.Cancel(ctx, stale, "conformance stale fence"); err == nil {
		t.Fatal("cancel accepted a stale fencing token")
	}

	// The live attempt is untouched: the stale caller neither replaced it nor
	// stopped it.
	live := observation(t, ctx, launcher, envelope)
	if live.ExternalID != current.ExternalID {
		t.Fatalf("stale fence displaced the live workload: %q != %q", live.ExternalID, current.ExternalID)
	}
	if live.State.Terminal() && live.State != current.State {
		t.Fatalf("stale fence changed the live state: %q -> %q", current.State, live.State)
	}
}

func observation(t *testing.T, ctx context.Context, launcher orchestration.Launcher, envelope orchestration.WorkloadEnvelope) orchestration.Observation {
	t.Helper()
	observed, err := launcher.Observe(ctx, envelope)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if err := observed.Validate(); err != nil {
		t.Fatalf("observation is not valid: %v", err)
	}
	return observed
}

// tracker enforces the ordering rules across a run of observations.
//
// Sequence is required to be non-decreasing rather than strictly increasing,
// because a backend whose sequence encodes observed progress reports the same
// number twice for an unchanged workload. What must never happen is a sequence
// going backwards, or a state changing without the sequence moving: either one
// makes a late observation indistinguishable from a current one.
type tracker struct {
	t          *testing.T
	externalID string
	count      int
	previous   orchestration.Observation
}

func (track *tracker) record(observed orchestration.Observation, err error) {
	track.t.Helper()
	if err != nil {
		track.t.Fatalf("observe: %v", err)
	}
	if err := observed.Validate(); err != nil {
		track.t.Fatalf("observation is not valid: %v", err)
	}
	if observed.ExternalID != track.externalID {
		track.t.Fatalf("observation names %q, launch named %q", observed.ExternalID, track.externalID)
	}
	if track.count > 0 {
		previous := track.previous
		if observed.Sequence < previous.Sequence {
			track.t.Fatalf("observation sequence regressed: %d < %d", observed.Sequence, previous.Sequence)
		}
		if observed.State != previous.State && observed.Sequence == previous.Sequence {
			track.t.Fatalf("state moved %q -> %q without advancing sequence %d", previous.State, observed.State, observed.Sequence)
		}
		if previous.State.Terminal() && observed.State != previous.State {
			track.t.Fatalf("terminal state %q changed to %q", previous.State, observed.State)
		}
		if observed.ObservedAt.Before(previous.ObservedAt) {
			track.t.Fatalf("observation time went backwards: %s < %s", observed.ObservedAt, previous.ObservedAt)
		}
	}
	if observed.State.Terminal() && observed.Failure == nil && observed.State != orchestration.AttemptCompleted {
		// Cancelled and failed are unsuccessful terminal states, and the
		// contract says Failure carries the cause for exactly those.
		track.t.Fatalf("terminal state %q carries no failure", observed.State)
	}
	track.previous = observed
	track.count++
}

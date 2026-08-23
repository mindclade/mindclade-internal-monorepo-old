// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package local runs bounded local workloads behind the orchestration Launcher
// contract.
//
// It exists so a developer, a component test, and the fleet can drive the same
// workflow. That is only true if this adapter honours the awkward parts of the
// contract rather than the happy path: duplicate delivery is idempotent, a
// cancellation that arrives after the work finished does not rewrite the
// outcome, and an envelope carrying a superseded fence is refused.
//
// Two properties are structural rather than incidental. First, the package
// starts no goroutines: control/ is checked for detached work, and a launcher
// that spawned a supervisor would own lifecycle it has no way to shut down. So
// Launch runs the workload inline and returns a terminal state. Second, the
// process itself is built by an injected CommandFactory, so the conformance
// suite exercises every branch here without forking a binary.
package local

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

// operation is the faults.WithOperation value for every fault raised here.
const operation = "control.orchestration.local"

// DefaultTrackedWorkloads bounds the launcher's registry.
//
// It fails closed rather than evicting. Evicting a terminal record would make
// the launcher forget that an attempt already ran, and the next duplicate
// delivery of that work item would start it again -- which is precisely the
// defect the registry exists to prevent. A process that genuinely reaches this
// bound is leaking workloads, and saying so is more useful than silently
// re-running one.
const DefaultTrackedWorkloads = 1024

// ExternalIDPrefix distinguishes this launcher's handles in telemetry.
const ExternalIDPrefix = "local-"

// externalIDDigestLength is how much of the identity digest the handle carries.
// 32 hex characters is 128 bits, which is far beyond any collision this process
// could produce, and it keeps the handle short enough to read in a log line.
const externalIDDigestLength = 32

// ExternalID is the deterministic handle for one envelope.
//
// It is a digest of the attempt's identity rather than a random value, because
// the contract requires a retried launch to address the same workload. A handle
// minted at launch time would make the second delivery of a work item
// unrecognisable, and the launcher would start a second copy of work that is
// already running.
func ExternalID(envelope orchestration.WorkloadEnvelope) string {
	preimage := strings.Join([]string{
		envelope.WorkloadID,
		envelope.RunID,
		envelope.JobID,
		envelope.StageID,
		strconv.FormatUint(uint64(envelope.Attempt), 10),
	}, "\x1f")
	return ExternalIDPrefix + identifiers.SHA256String(preimage).Hex()[:externalIDDigestLength]
}

// Option configures a Launcher.
type Option func(*Launcher) error

// WithClock replaces the time source.
func WithClock(source clock.Clock) Option {
	return func(launcher *Launcher) error {
		if nilInterface(source) {
			return invalid("clock_nil", "clock is required", nil)
		}
		launcher.clock = source
		return nil
	}
}

// WithTrackedWorkloads overrides the registry bound.
func WithTrackedWorkloads(maximum int) Option {
	return func(launcher *Launcher) error {
		if maximum <= 0 {
			return invalid("tracked_workloads_invalid", "tracked workload bound must be positive", nil)
		}
		launcher.maximumTracked = maximum
		return nil
	}
}

// Launcher is the local implementation of orchestration.Launcher.
type Launcher struct {
	commands       CommandFactory
	clock          clock.Clock
	maximumTracked int

	mutex   sync.Mutex
	records map[string]*record
}

var _ orchestration.Launcher = (*Launcher)(nil)

// New builds a launcher over an injected command factory.
func New(commands CommandFactory, options ...Option) (*Launcher, error) {
	if nilInterface(commands) {
		return nil, unavailable("command_factory_unavailable", "local command factory is unavailable", nil)
	}
	launcher := &Launcher{
		commands:       commands,
		clock:          clock.RealClock{},
		maximumTracked: DefaultTrackedWorkloads,
		records:        make(map[string]*record),
	}
	for _, option := range options {
		if option == nil {
			return nil, invalid("option_nil", "launcher option is nil", nil)
		}
		if err := option(launcher); err != nil {
			return nil, err
		}
	}
	return launcher, nil
}

// record is the launcher's durable-enough memory of one workload.
type record struct {
	externalID string
	fence      uint64
	state      orchestration.AttemptState
	sequence   uint64
	failure    error
	result     Result
}

// advance moves the record through the worker protocol's transition table.
//
// It refuses an edge the table forbids instead of assigning the state anyway.
// The local launcher is the cheapest place to notice that a lifecycle was
// written wrong, and a state the Rust worker could never report is a bug
// whichever side produced it.
func (entry *record) advance(to orchestration.AttemptState) error {
	if !orchestration.CanTransition(entry.state, to) {
		return failedPrecondition("attempt_transition_invalid",
			"local workload cannot move from "+string(entry.state)+" to "+string(to))
	}
	entry.state = to
	entry.sequence++
	return nil
}

// Launch starts the workload, or reports that it is already present.
func (launcher *Launcher) Launch(ctx context.Context, envelope orchestration.WorkloadEnvelope) (orchestration.LaunchOutcome, error) {
	if err := launcher.admit(ctx, envelope); err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	entry, existed, err := launcher.reserve(envelope)
	if err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	if existed {
		launcher.mutex.Lock()
		outcome := orchestration.LaunchOutcome{ExternalID: entry.externalID, Existed: true, State: entry.state}
		launcher.mutex.Unlock()
		return outcome, nil
	}
	return launcher.execute(ctx, envelope, entry)
}

// admit is the guard every method shares: a usable context and an envelope
// whose ticket is active right now.
func (launcher *Launcher) admit(ctx context.Context, envelope orchestration.WorkloadEnvelope) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return canceled(err)
	}
	return envelope.Validate(launcher.clock.Now())
}

// reserve inserts the record for a new workload, or returns the existing one.
//
// The fence comparison is the fencing rule stated for a launcher. An envelope
// carrying an older fence belongs to a replica that has already been replaced,
// and honouring it would let the old owner address work the new owner holds.
// A newer fence supersedes, because recovery mints a new ticket with a higher
// fence and that ticket is the authority.
func (launcher *Launcher) reserve(envelope orchestration.WorkloadEnvelope) (*record, bool, error) {
	fence := envelope.ExecutionTicket.Claims.FencingToken
	launcher.mutex.Lock()
	defer launcher.mutex.Unlock()
	if existing, found := launcher.records[envelope.WorkloadID]; found {
		switch {
		case fence < existing.fence:
			return nil, false, conflict("stale_fencing_token", "launch carries a fencing token an existing owner has superseded")
		case fence == existing.fence:
			return existing, true, nil
		}
		delete(launcher.records, envelope.WorkloadID)
	}
	if len(launcher.records) >= launcher.maximumTracked {
		return nil, false, exhausted("tracked_workload_bound", "the local launcher is tracking its maximum number of workloads")
	}
	entry := &record{
		externalID: ExternalID(envelope),
		fence:      fence,
		state:      orchestration.AttemptCreated,
		sequence:   1,
	}
	launcher.records[envelope.WorkloadID] = entry
	return entry, false, nil
}

// execute walks the attempt through the worker states and runs it inline.
//
// The registry lock is released across the run so a concurrent Observe reports
// progress instead of blocking behind the workload it is asking about. Every
// write after the run re-checks that this record is still the registry's, since
// a newer fence may have superseded it while the process was executing.
func (launcher *Launcher) execute(ctx context.Context, envelope orchestration.WorkloadEnvelope, entry *record) (orchestration.LaunchOutcome, error) {
	if err := launcher.transition(envelope, entry, orchestration.AttemptStarting); err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	command, err := launcher.commands.NewCommand(ctx, envelope)
	if err != nil {
		launcher.fail(envelope, entry, err)
		return orchestration.LaunchOutcome{}, err
	}
	if nilInterface(command) {
		failure := unavailable("command_unavailable", "local command factory returned no command", nil)
		launcher.fail(envelope, entry, failure)
		return orchestration.LaunchOutcome{}, failure
	}
	for _, next := range []orchestration.AttemptState{orchestration.AttemptReady, orchestration.AttemptLeased, orchestration.AttemptRunning} {
		if err := launcher.transition(envelope, entry, next); err != nil {
			return orchestration.LaunchOutcome{}, err
		}
	}

	result, runErr := command.Run(ctx)
	launcher.mutex.Lock()
	defer launcher.mutex.Unlock()
	if launcher.records[envelope.WorkloadID] != entry || entry.state != orchestration.AttemptRunning {
		// Superseded or cancelled while the process was running. The winner's
		// record is authoritative, so this one reports what is there now.
		return orchestration.LaunchOutcome{ExternalID: entry.externalID, Existed: true, State: entry.state}, nil
	}
	entry.result = result
	switch {
	case runErr != nil:
		entry.failure = runErr
		if err := entry.advance(orchestration.AttemptFailed); err != nil {
			return orchestration.LaunchOutcome{}, err
		}
		return orchestration.LaunchOutcome{}, runErr
	case result.ExitCode != 0:
		entry.failure = failedExit(result.ExitCode)
		if err := entry.advance(orchestration.AttemptFailed); err != nil {
			return orchestration.LaunchOutcome{}, err
		}
	default:
		for _, next := range []orchestration.AttemptState{orchestration.AttemptCommitting, orchestration.AttemptCompleted} {
			if err := entry.advance(next); err != nil {
				return orchestration.LaunchOutcome{}, err
			}
		}
	}
	return orchestration.LaunchOutcome{ExternalID: entry.externalID, Existed: false, State: entry.state}, nil
}

// transition applies one state change under the registry lock.
func (launcher *Launcher) transition(envelope orchestration.WorkloadEnvelope, entry *record, to orchestration.AttemptState) error {
	launcher.mutex.Lock()
	defer launcher.mutex.Unlock()
	if launcher.records[envelope.WorkloadID] != entry {
		return conflict("workload_superseded", "the workload record was replaced by a newer fence")
	}
	return entry.advance(to)
}

// fail records a launch-time failure without discarding the record. The record
// is what makes the next duplicate delivery idempotent, so a failed attempt has
// to stay visible rather than being rolled back.
func (launcher *Launcher) fail(envelope orchestration.WorkloadEnvelope, entry *record, cause error) {
	launcher.mutex.Lock()
	defer launcher.mutex.Unlock()
	if launcher.records[envelope.WorkloadID] != entry || entry.state.Terminal() {
		return
	}
	entry.failure = cause
	_ = entry.advance(orchestration.AttemptFailed)
}

// Observe reads the recorded state of a previously launched workload.
func (launcher *Launcher) Observe(ctx context.Context, envelope orchestration.WorkloadEnvelope) (orchestration.Observation, error) {
	if err := launcher.admit(ctx, envelope); err != nil {
		return orchestration.Observation{}, err
	}
	observedAt := launcher.clock.Now()
	launcher.mutex.Lock()
	defer launcher.mutex.Unlock()
	entry, err := launcher.lookup(envelope)
	if err != nil {
		return orchestration.Observation{}, err
	}
	return orchestration.Observation{
		ExternalID: entry.externalID,
		State:      entry.state,
		Sequence:   entry.sequence,
		Failure:    entry.failure,
		ObservedAt: observedAt,
	}, nil
}

// Cancel stops the workload, honouring the fence.
func (launcher *Launcher) Cancel(ctx context.Context, envelope orchestration.WorkloadEnvelope, reason string) error {
	if err := launcher.admit(ctx, envelope); err != nil {
		return err
	}
	if err := validateReason(reason); err != nil {
		return err
	}
	launcher.mutex.Lock()
	defer launcher.mutex.Unlock()
	entry, found := launcher.records[envelope.WorkloadID]
	if !found {
		// Nothing to stop. A cancellation that raced the launcher losing its
		// claim arrives exactly like this, and failing it would strand the
		// stage on a fault nobody can act on.
		return nil
	}
	if envelope.ExecutionTicket.Claims.FencingToken < entry.fence {
		return conflict("stale_fencing_token", "cancellation carries a fencing token an existing owner has superseded")
	}
	if entry.state.Terminal() {
		// The attempt already published an outcome. Overwriting it here would
		// be a deletion wearing another name.
		return nil
	}
	if entry.state == orchestration.AttemptCancelling {
		return nil
	}
	if err := entry.advance(orchestration.AttemptCancelling); err != nil {
		return err
	}
	entry.failure = cancelledBy(reason)
	return entry.advance(orchestration.AttemptCancelled)
}

// Result returns the captured output of a finished workload. It is the reason
// a developer runs against this adapter at all, and it is bounded by
// MaximumCapturedBytes rather than by what the workload chose to print.
func (launcher *Launcher) Result(envelope orchestration.WorkloadEnvelope) (Result, error) {
	launcher.mutex.Lock()
	defer launcher.mutex.Unlock()
	entry, err := launcher.lookup(envelope)
	if err != nil {
		return Result{}, err
	}
	if !entry.state.Terminal() {
		return Result{}, failedPrecondition("workload_not_finished", "the local workload has not finished")
	}
	return entry.result, nil
}

// lookup resolves the record for an envelope. Callers hold the registry lock.
func (launcher *Launcher) lookup(envelope orchestration.WorkloadEnvelope) (*record, error) {
	entry, found := launcher.records[envelope.WorkloadID]
	if !found {
		return nil, notFound("workload_not_found", "no local workload was launched for this envelope")
	}
	if envelope.ExecutionTicket.Claims.FencingToken < entry.fence {
		return nil, conflict("stale_fencing_token", "observation carries a fencing token an existing owner has superseded")
	}
	return entry, nil
}

func validateReason(reason string) error {
	if reason == "" || len(reason) > orchestration.MaximumReasonLength || reason != strings.TrimSpace(reason) {
		return invalid("reason_invalid", "cancellation reason is empty, oversized, or not trimmed", nil)
	}
	return nil
}

func failedExit(code int) error {
	return faults.New(faults.CodeInternal, "local workload exited with a non-zero status",
		faults.WithReason("local_workload_failed"), faults.WithOperation(operation),
		faults.WithFields(faults.Fields{"exit_code": code}),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func cancelledBy(reason string) error {
	return faults.New(faults.CodeCanceled, "local workload was cancelled",
		faults.WithReason("local_workload_cancelled"), faults.WithOperation(operation),
		faults.WithFields(faults.Fields{"cancellation_reason": reason}),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func canceled(cause error) error {
	return faults.Wrap(cause, faults.CodeCanceled, "local launcher request was cancelled",
		faults.WithReason("request_canceled"), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func invalid(reason, message string, cause error) error {
	options := []faults.Option{
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()),
	}
	if cause != nil {
		return faults.Wrap(cause, faults.CodeInvalidArgument, message, options...)
	}
	return faults.New(faults.CodeInvalidArgument, message, options...)
}

func notFound(reason, message string) error {
	return faults.New(faults.CodeNotFound, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func conflict(reason, message string) error {
	return faults.New(faults.CodeConflict, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func failedPrecondition(reason, message string) error {
	return faults.New(faults.CodeFailedPrecondition, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func exhausted(reason, message string) error {
	return faults.New(faults.CodeResourceExhausted, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()))
}

// unavailable is the only helper here that admits a retry: a process that could
// not be started may start on the next attempt, whereas every other refusal in
// this package is a decision that replaying cannot change.
func unavailable(reason, message string, cause error) error {
	options := []faults.Option{
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.BackoffRetry(3)),
	}
	if cause != nil {
		return faults.Wrap(cause, faults.CodeUnavailable, message, options...)
	}
	return faults.New(faults.CodeUnavailable, message, options...)
}

// nilInterface reports whether an interface holds a nil pointer. A typed nil is
// not == nil, so a launcher handed a (*ExecCommands)(nil) would pass a plain
// guard and panic on first use.
func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

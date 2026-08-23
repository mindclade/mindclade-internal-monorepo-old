// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package slurm translates orchestration requests into Slurm job operations.
//
// It is a translation layer and nothing else. It holds no connection, runs no
// poll loop, and shells out to nothing: the three Slurm verbs it needs are an
// injected Client, and everything in this file is a pure projection between an
// orchestration envelope and that interface.
//
// The awkward part of speaking Slurm is that it has no idempotency key. sbatch
// happily submits the same script twice, so a duplicate work-item delivery
// would produce two jobs charging the same allocation. This package closes that
// by addressing jobs through a deterministic name derived from the attempt's
// identity and by querying before it submits. The fence travels in the job
// comment for the same reason: a job started by a replica that has since been
// replaced must be recognisable as such by whoever replaced it.
package slurm

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

// operation is the faults.WithOperation value for every fault raised here.
const operation = "control.orchestration.slurm"

const (
	// JobNamePrefix marks a job this control plane owns. Slurm partitions are
	// routinely shared with jobs submitted by hand, and an operator reading
	// squeue has to be able to tell which ones a controller will reconcile.
	JobNamePrefix = "mc-"
	// jobNameDigestLength keeps the name inside Slurm's practical name budget
	// while carrying 128 bits of attempt identity.
	jobNameDigestLength = 32

	// CommentPrefix opens the structured job comment.
	CommentPrefix = "mindclade"
	// commentSeparator joins the comment's key=value pairs. Slurm passes the
	// comment through unparsed, so the separator only has to avoid whitespace.
	commentSeparator = ";"
)

// MaximumRevision bounds the provider-reported revision so the sequence
// arithmetic below cannot overflow. A Slurm accounting revision this large is a
// provider defect, and multiplying it into a uint64 silently would produce an
// observation sequence that wraps and starts ordering backwards.
const MaximumRevision = uint64(1) << 56

// stateRankSpan is the multiplier that lets one sequence carry both the
// provider's revision and the lifecycle position, with the revision dominating.
const stateRankSpan = uint64(16)

// State is a Slurm job state name. Slurm reports these upper-case; the
// projection normalises before comparing so a provider that lower-cases them is
// not silently mapped to "unknown".
type State string

// The Slurm states this adapter recognises. Anything else is refused rather
// than guessed: a state nobody mapped is a state whose terminality is unknown,
// and guessing wrong either strands a stage or declares a running job dead.
const (
	StatePending     State = "PENDING"
	StateConfiguring State = "CONFIGURING"
	StateRequeued    State = "REQUEUED"
	StateResizing    State = "RESIZING"
	StateRunning     State = "RUNNING"
	StateSuspended   State = "SUSPENDED"
	StateStopped     State = "STOPPED"
	StateSignaling   State = "SIGNALING"
	StateStageOut    State = "STAGE_OUT"
	StateCompleting  State = "COMPLETING"
	StateCompleted   State = "COMPLETED"
	StateCancelled   State = "CANCELLED"
	StateRevoked     State = "REVOKED"
	StateFailed      State = "FAILED"
	StateTimeout     State = "TIMEOUT"
	StateDeadline    State = "DEADLINE"
	StateNodeFail    State = "NODE_FAIL"
	StateBootFail    State = "BOOT_FAIL"
	StateOutOfMemory State = "OUT_OF_MEMORY"
	StatePreempted   State = "PREEMPTED"
	StateSpecialExit State = "SPECIAL_EXIT"
)

// stateRankTerminal is the rank every terminal state shares. Terminal states do
// not order among themselves: an attempt reaches exactly one of them and stops.
const stateRankTerminal = uint64(10)

// Reference addresses one Slurm job.
//
// Name is authoritative and Mindclade owns it; ID is whatever Slurm assigned
// and is provider-internal. The launcher never keys on ID because Slurm reissues
// it on requeue, which would make an attempt lose track of its own job at the
// exact moment recovery matters.
type Reference struct {
	Name string
	ID   string
}

// Job is one Slurm job as the provider reports it.
type Job struct {
	Reference Reference
	State     State
	// Reason is Slurm's own explanation, carried into the failure fault so an
	// operator sees "QOSMaxJobsPerUserLimit" rather than "the job failed".
	Reason string
	// Fence is the fencing token recovered from the job comment. A provider
	// that cannot recover it must report zero, which this package treats as an
	// unfenced job rather than as a matching fence.
	Fence uint64
	// Revision orders observations of one job. The provider must report a value
	// that never decreases for a given job -- an accounting revision, a poll
	// counter, anything monotone -- because the orchestration contract requires
	// observations to be orderable and Slurm state alone cannot express that a
	// job was requeued and is pending again.
	Revision uint64
	ExitCode int32
}

// Submission is everything about a Slurm job that orchestration does not
// decide: the partition it runs in, the account it charges, and the script it
// executes. Resolving it is policy, and policy does not belong in an adapter.
type Submission struct {
	Partition   string
	Account     string
	QoS         string
	Nodes       uint32
	Script      string
	Environment []string
	// GPUs is the device count for the generic resource request. It is here
	// rather than derived from the ticket because the execution budget bounds
	// GPU *memory*, and dividing one by an assumed device size would invent a
	// device count nobody authorised.
	GPUs uint32
}

// SubmitRequest is the complete sbatch this adapter asks for.
type SubmitRequest struct {
	Reference Reference
	Comment   string
	Fence     uint64
	TimeLimit time.Duration
	// CPUs is the ticket's CPU budget rounded up to whole cores, because Slurm
	// allocates cores and rounding down would hand the workload less than its
	// ticket granted.
	CPUs           uint32
	MemoryBytes    uint64
	LocalDiskBytes uint64
	Submission     Submission
}

// Client is the narrow Slurm surface this package needs. The composition root
// supplies it; this package neither opens a connection nor runs a binary.
//
// A missing job must be reported as a faults.CodeNotFound fault. That is the
// one contract detail a provider cannot get wrong without breaking idempotence:
// the launcher reads "not found" as "nothing was submitted yet", and a provider
// that returned a zero Job instead would cause a second submission.
type Client interface {
	Submit(ctx context.Context, request SubmitRequest) (Job, error)
	Query(ctx context.Context, reference Reference) (Job, error)
	Cancel(ctx context.Context, reference Reference, reason string) error
}

// Resolver turns an envelope into the Slurm submission that runs it.
type Resolver interface {
	Resolve(orchestration.WorkloadEnvelope) (Submission, error)
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

// Launcher is the Slurm implementation of orchestration.Launcher.
type Launcher struct {
	client   Client
	resolver Resolver
	clock    clock.Clock
}

var _ orchestration.Launcher = (*Launcher)(nil)

// New builds a launcher over an injected client and submission resolver.
func New(client Client, resolver Resolver, options ...Option) (*Launcher, error) {
	if nilInterface(client) {
		return nil, unavailable("client_unavailable", "slurm client is unavailable", nil)
	}
	if nilInterface(resolver) {
		return nil, unavailable("resolver_unavailable", "slurm submission resolver is unavailable", nil)
	}
	launcher := &Launcher{client: client, resolver: resolver, clock: clock.RealClock{}}
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

// JobName is the deterministic Slurm job name for one envelope.
//
// It is a digest of the attempt's identity rather than a readable composition
// of its fields, because Slurm bounds job names and a truncated readable name
// would collide between two attempts of the same stage.
func JobName(envelope orchestration.WorkloadEnvelope) string {
	preimage := strings.Join([]string{
		envelope.WorkloadID,
		envelope.RunID,
		envelope.JobID,
		envelope.StageID,
		strconv.FormatUint(uint64(envelope.Attempt), 10),
	}, "\x1f")
	return JobNamePrefix + identifiers.SHA256String(preimage).Hex()[:jobNameDigestLength]
}

// Comment renders the structured job comment.
//
// The fence is in the comment because Slurm has nowhere else to put it: there
// are no labels, no annotations, and no owner references. Without it a replaced
// replica could not tell its own job from its successor's.
func Comment(envelope orchestration.WorkloadEnvelope) string {
	return strings.Join([]string{
		CommentPrefix,
		"workload=" + envelope.WorkloadID,
		"attempt=" + strconv.FormatUint(uint64(envelope.Attempt), 10),
		"fence=" + strconv.FormatUint(envelope.ExecutionTicket.Claims.FencingToken, 10),
	}, commentSeparator)
}

// Launch submits the job, or reports that it is already queued.
func (launcher *Launcher) Launch(ctx context.Context, envelope orchestration.WorkloadEnvelope) (orchestration.LaunchOutcome, error) {
	if err := launcher.admit(ctx, envelope); err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	reference := Reference{Name: JobName(envelope)}
	existing, found, err := launcher.find(ctx, envelope, reference)
	if err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	if found {
		projected, err := Project(existing)
		if err != nil {
			return orchestration.LaunchOutcome{}, err
		}
		return orchestration.LaunchOutcome{ExternalID: reference.Name, Existed: true, State: projected.State}, nil
	}
	request, err := launcher.submitRequest(envelope, reference)
	if err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	submitted, err := launcher.client.Submit(ctx, request)
	if err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	// A provider that renamed the job would break every subsequent Query, and
	// the failure would look like the job vanishing rather than like a bad
	// adapter.
	if submitted.Reference.Name != reference.Name {
		return orchestration.LaunchOutcome{}, failedPrecondition("submitted_name_mismatch", "the slurm provider submitted a job under a different name")
	}
	projected, err := Project(submitted)
	if err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	return orchestration.LaunchOutcome{ExternalID: reference.Name, Existed: false, State: projected.State}, nil
}

// Observe reads the current state of a previously submitted job.
func (launcher *Launcher) Observe(ctx context.Context, envelope orchestration.WorkloadEnvelope) (orchestration.Observation, error) {
	if err := launcher.admit(ctx, envelope); err != nil {
		return orchestration.Observation{}, err
	}
	observedAt := launcher.clock.Now()
	reference := Reference{Name: JobName(envelope)}
	job, found, err := launcher.find(ctx, envelope, reference)
	if err != nil {
		return orchestration.Observation{}, err
	}
	if !found {
		return orchestration.Observation{}, notFound("job_not_found", "no slurm job was submitted for this envelope")
	}
	projected, err := Project(job)
	if err != nil {
		return orchestration.Observation{}, err
	}
	sequence, err := Sequence(job)
	if err != nil {
		return orchestration.Observation{}, err
	}
	return orchestration.Observation{
		ExternalID: reference.Name,
		State:      projected.State,
		Sequence:   sequence,
		Failure:    projected.Failure,
		ObservedAt: observedAt,
	}, nil
}

// Cancel stops the job. A job that is already gone, or already finished, is not
// an error: a cancellation that raced a completion must not fail.
func (launcher *Launcher) Cancel(ctx context.Context, envelope orchestration.WorkloadEnvelope, reason string) error {
	if err := launcher.admit(ctx, envelope); err != nil {
		return err
	}
	if err := validateReason(reason); err != nil {
		return err
	}
	reference := Reference{Name: JobName(envelope)}
	job, found, err := launcher.find(ctx, envelope, reference)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	projected, err := Project(job)
	if err != nil {
		return err
	}
	if projected.State.Terminal() {
		return nil
	}
	return launcher.client.Cancel(ctx, job.Reference, reason)
}

// admit is the guard every method shares.
func (launcher *Launcher) admit(ctx context.Context, envelope orchestration.WorkloadEnvelope) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return canceled(err)
	}
	return envelope.Validate(launcher.clock.Now())
}

// find queries the job and enforces the fence.
//
// A job whose recorded fence is newer than the caller's belongs to a replica
// that replaced this one. Answering the stale caller with the job's state would
// let it cancel work it no longer owns, so it is refused with a fault that ends
// the attempt rather than one that schedules a retry.
func (launcher *Launcher) find(ctx context.Context, envelope orchestration.WorkloadEnvelope, reference Reference) (Job, bool, error) {
	job, err := launcher.client.Query(ctx, reference)
	if err != nil {
		if faults.IsCode(err, faults.CodeNotFound) {
			return Job{}, false, nil
		}
		return Job{}, false, err
	}
	if job.Reference.Name != reference.Name {
		return Job{}, false, failedPrecondition("queried_name_mismatch", "the slurm provider returned a job under a different name")
	}
	if envelope.ExecutionTicket.Claims.FencingToken < job.Fence {
		return Job{}, false, conflict("stale_fencing_token", "the request carries a fencing token an existing owner has superseded")
	}
	return job, true, nil
}

// submitRequest projects the envelope and the resolved submission onto sbatch.
func (launcher *Launcher) submitRequest(envelope orchestration.WorkloadEnvelope, reference Reference) (SubmitRequest, error) {
	submission, err := launcher.resolver.Resolve(envelope)
	if err != nil {
		return SubmitRequest{}, err
	}
	if err := validateSubmission(submission, envelope); err != nil {
		return SubmitRequest{}, err
	}
	budget := envelope.ExecutionTicket.Claims.Budget
	if err := budget.Validate(); err != nil {
		return SubmitRequest{}, err
	}
	limit := envelope.Deadline.Sub(launcher.clock.Now())
	if limit <= 0 {
		return SubmitRequest{}, invalid("time_limit_elapsed", "the workload deadline has already elapsed", nil)
	}
	disk, err := ephemeralBytes(budget)
	if err != nil {
		return SubmitRequest{}, err
	}
	return SubmitRequest{
		Reference:      reference,
		Comment:        Comment(envelope),
		Fence:          envelope.ExecutionTicket.Claims.FencingToken,
		TimeLimit:      limit,
		CPUs:           wholeCores(budget.CPUMillis),
		MemoryBytes:    budget.ResidentMemoryBytes,
		LocalDiskBytes: disk,
		Submission:     submission,
	}, nil
}

// wholeCores rounds a millicore budget up. Slurm allocates whole cores, so
// rounding down would run the workload under the bound its ticket granted.
func wholeCores(millis uint32) uint32 {
	cores := millis / 1000
	if millis%1000 != 0 {
		cores++
	}
	if cores == 0 {
		cores = 1
	}
	return cores
}

// ephemeralBytes is every local-disk consumer the ticket bounds, summed.
//
// Checkpoint staging and the telemetry spool land on the same filesystem as the
// working set, so a request covering only LocalDiskBytes would let a checkpoint
// fill the node out from under the job that wrote it. The sum is overflow
// checked because these are three independent uint64 grants and a wrapped total
// would ask Slurm for almost nothing.
func ephemeralBytes(budget runtime_authority.ExecutionBudget) (uint64, error) {
	total := uint64(0)
	for _, amount := range []uint64{budget.LocalDiskBytes, budget.CheckpointStagingBytes, budget.TelemetrySpoolBytes} {
		if total > mathMaxUint64-amount {
			return 0, invalid("local_disk_budget_overflow", "the ticket local-disk budgets sum beyond a uint64", nil)
		}
		total += amount
	}
	if total == 0 {
		return 0, invalid("local_disk_budget_required", "the ticket grants no local disk for the workload", nil)
	}
	return total, nil
}

// mathMaxUint64 is spelled out rather than imported so the overflow guard above
// reads as arithmetic rather than as a dependency.
const mathMaxUint64 = ^uint64(0)

func validateSubmission(submission Submission, envelope orchestration.WorkloadEnvelope) error {
	if strings.TrimSpace(submission.Partition) == "" {
		return invalid("partition_required", "slurm submission requires a partition", nil)
	}
	if strings.TrimSpace(submission.Script) == "" {
		return invalid("script_required", "slurm submission requires a script", nil)
	}
	if submission.Nodes == 0 {
		return invalid("nodes_required", "slurm submission requires at least one node", nil)
	}
	for _, value := range submission.Environment {
		if !strings.Contains(value, "=") {
			return invalid("environment_invalid", "slurm environment entries must be key=value", nil)
		}
	}
	// A device count and a GPU memory bound have to agree. Requesting devices a
	// ticket did not budget for is capacity theft; budgeting GPU memory and
	// requesting no device is a job that will never find one.
	estimate := envelope.ExecutionTicket.Claims.Budget.GPUMemoryEstimateBytes
	if (submission.GPUs > 0) != (estimate > 0) {
		return failedPrecondition("gpu_request_mismatch", "the requested device count and the ticket's gpu memory budget disagree")
	}
	return nil
}

// Projection is one Slurm job read as an attempt.
type Projection struct {
	State orchestration.AttemptState
	// Failure explains an unsuccessful terminal state. Its fault code carries
	// the disposition: a preempted job is backpressure and a failed one is not.
	Failure error
}

// Project maps a Slurm state onto the attempt vocabulary and, for unsuccessful
// terminal states, the failure that explains it.
//
// The mapping is exhaustive rather than defaulted. An unrecognised Slurm state
// is refused, because the only safe default would be "still running" and that
// would hold a stage open on a job that is gone.
func Project(job Job) (Projection, error) {
	switch normalize(job.State) {
	case StatePending:
		return Projection{State: orchestration.AttemptCreated}, nil
	case StateConfiguring:
		return Projection{State: orchestration.AttemptStarting}, nil
	case StateRequeued, StateResizing:
		return Projection{State: orchestration.AttemptRecovering}, nil
	case StateRunning:
		return Projection{State: orchestration.AttemptRunning}, nil
	case StateSuspended, StateStopped:
		return Projection{State: orchestration.AttemptDraining}, nil
	case StateSignaling:
		return Projection{State: orchestration.AttemptCancelling}, nil
	case StateStageOut, StateCompleting:
		return Projection{State: orchestration.AttemptCommitting}, nil
	case StateCompleted:
		return Projection{State: orchestration.AttemptCompleted}, nil
	case StateCancelled, StateRevoked:
		return Projection{State: orchestration.AttemptCancelled, Failure: terminalFailure(faults.CodeCanceled, "slurm_job_cancelled", "the slurm job was cancelled", job)}, nil
	case StatePreempted:
		// Preemption is capacity backpressure, not a defect in the work, so it
		// carries the code orchestration.Classify turns into a reschedule
		// rather than one that charges the stage an attempt.
		return Projection{State: orchestration.AttemptFailed, Failure: terminalFailure(faults.CodeResourceExhausted, "slurm_job_preempted", "the slurm job was preempted", job)}, nil
	case StateTimeout, StateDeadline:
		return Projection{State: orchestration.AttemptFailed, Failure: terminalFailure(faults.CodeDeadlineExceeded, "slurm_job_timed_out", "the slurm job exceeded its time limit", job)}, nil
	case StateNodeFail, StateBootFail:
		return Projection{State: orchestration.AttemptFailed, Failure: terminalFailure(faults.CodeUnavailable, "slurm_node_failed", "the slurm job lost its nodes", job)}, nil
	case StateOutOfMemory:
		return Projection{State: orchestration.AttemptFailed, Failure: terminalFailure(faults.CodeResourceExhausted, "slurm_job_out_of_memory", "the slurm job exhausted its memory budget", job)}, nil
	case StateFailed, StateSpecialExit:
		return Projection{State: orchestration.AttemptFailed, Failure: terminalFailure(faults.CodeInternal, "slurm_job_failed", "the slurm job failed", job)}, nil
	default:
		return Projection{}, failedPrecondition("job_state_unrecognized", "the slurm provider reported a state this adapter does not map")
	}
}

// Sequence orders observations of one job.
//
// The revision dominates and the lifecycle rank breaks ties within it. Rank
// alone would not do: a requeued job moves from RUNNING back to PENDING, and a
// sequence built from lifecycle position would go backwards at exactly the
// moment a stale observation is most likely to be in flight.
func Sequence(job Job) (uint64, error) {
	if job.Revision == 0 {
		return 0, failedPrecondition("job_revision_missing", "the slurm provider reported no observation revision")
	}
	if job.Revision > MaximumRevision {
		return 0, failedPrecondition("job_revision_out_of_range", "the slurm provider reported a revision beyond the adapter bound")
	}
	projected, err := Project(job)
	if err != nil {
		return 0, err
	}
	return job.Revision*stateRankSpan + stateRank(projected.State), nil
}

// stateRank is the lifecycle position of an attempt state, used only to order
// two observations that share a revision.
func stateRank(state orchestration.AttemptState) uint64 {
	switch state {
	case orchestration.AttemptCreated:
		return 1
	case orchestration.AttemptStarting:
		return 2
	case orchestration.AttemptRecovering:
		return 3
	case orchestration.AttemptReady:
		return 4
	case orchestration.AttemptLeased:
		return 5
	case orchestration.AttemptRunning:
		return 6
	case orchestration.AttemptDraining:
		return 7
	case orchestration.AttemptCancelling:
		return 8
	case orchestration.AttemptCommitting:
		return 9
	default:
		return stateRankTerminal
	}
}

func normalize(state State) State {
	return State(strings.ToUpper(strings.TrimSpace(string(state))))
}

func terminalFailure(code faults.Code, reason, message string, job Job) error {
	fields := faults.Fields{"slurm.job_name": job.Reference.Name, "slurm.exit_code": job.ExitCode}
	if job.Reference.ID != "" {
		fields["slurm.job_id"] = job.Reference.ID
	}
	if job.Reason != "" {
		fields["slurm.reason"] = job.Reason
	}
	return faults.New(code, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithFields(fields), faults.WithRetryPolicy(faults.NoRetry()))
}

func validateReason(reason string) error {
	if reason == "" || len(reason) > orchestration.MaximumReasonLength || reason != strings.TrimSpace(reason) {
		return invalid("reason_invalid", "cancellation reason is empty, oversized, or not trimmed", nil)
	}
	return nil
}

func canceled(cause error) error {
	return faults.Wrap(cause, faults.CodeCanceled, "slurm launcher request was cancelled",
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

// unavailable is the only helper here that admits a retry: a provider that is
// unreachable now may answer on the next attempt, whereas every other refusal
// in this package is a decision replaying cannot change.
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
// not == nil, so a launcher handed a (*provider)(nil) would pass a plain guard
// and panic on first use.
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

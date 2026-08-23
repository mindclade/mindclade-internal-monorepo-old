// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package kubernetes builds and reconciles JobSet/Kueue workload objects from
// orchestration decisions.
//
// It owns no scheduling policy, no workflow state, and no process lifecycle. It
// holds no client library either: the controller-runtime client is injected, so
// this package is a projection between an orchestration envelope and the exact
// object shape the cluster will accept.
//
// Three facts about that cluster drive everything here. The
// mindclade-restricted-pods policy denies on eleven separate pod conditions,
// mindclade-capacity-queue-contract denies any batch object whose namespace,
// queue-name label, and workload-class label are not one of three exact
// triples, and mindclade-block-job-activation denies an unsuspended Job or
// JobSet outright. All three are ValidatingAdmissionPolicies with Deny in
// validationActions, so an object that violates any of them is not "eventually
// reconciled" -- it is rejected, asynchronously, with the failure attached to
// an object rather than to the run that wanted it. Building and verifying the
// object here is what turns that into a synchronous, actionable refusal.
package kubernetes

import (
	"context"
	"reflect"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/kubernetes"
	"go.mindclade.dev/libs/go/kubernetes/metadata"
	"go.mindclade.dev/libs/go/kubernetes/patch"
)

// operation is the faults.WithOperation value for every fault raised here.
const operation = "control.orchestration.kubernetes"

// FieldOwner is this adapter's server-side-apply identity.
//
// It is a stable exported constant because field ownership outlives a release:
// changing it would orphan every field the previous owner managed, and the next
// apply would leave stale values in place rather than replacing them. Applies
// are made with force disabled, so a field another controller has taken over
// surfaces as a conflict instead of being silently stolen back.
const FieldOwner = "mindclade-orchestration-launcher"

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

// WithAdmissions wires the Kueue admission reader.
//
// Without it the launcher still satisfies the whole Launcher contract; what it
// cannot do is tell a workload waiting for quota apart from one whose quota was
// granted a moment ago. That distinction is reporting detail, not correctness,
// which is why it is optional rather than required.
func WithAdmissions(admissions Admissions) Option {
	return func(launcher *Launcher) error {
		if nilInterface(admissions) {
			return invalid("admissions_nil", "admission reader is required", nil)
		}
		launcher.admissions = admissions
		return nil
	}
}

// Launcher is the Kubernetes implementation of orchestration.Launcher.
type Launcher struct {
	client     crclient.Client
	resolver   Resolver
	admissions Admissions
	clock      clock.Clock
}

var _ orchestration.Launcher = (*Launcher)(nil)

// New builds a launcher over an injected client and pod-spec resolver.
func New(client crclient.Client, resolver Resolver, options ...Option) (*Launcher, error) {
	if nilInterface(client) {
		return nil, unavailable("client_unavailable", "kubernetes client is unavailable", nil)
	}
	if nilInterface(resolver) {
		return nil, unavailable("resolver_unavailable", "pod specification resolver is unavailable", nil)
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

// Launch applies the JobSet, or reports that it is already present.
//
// An object that already exists is never an error. Duplicate delivery is normal
// for an at-least-once queue, and failing here would burn the stage's attempt
// budget on work that is already running.
func (launcher *Launcher) Launch(ctx context.Context, envelope orchestration.WorkloadEnvelope) (orchestration.LaunchOutcome, error) {
	if err := launcher.admit(ctx, envelope); err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	key, err := launcher.key(envelope)
	if err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	live, found, err := launcher.get(ctx, key)
	if err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	if found {
		return launcher.existing(ctx, envelope, key, live)
	}
	spec, err := launcher.resolver.Resolve(envelope)
	if err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	object, err := Build(envelope, spec, launcher.clock.Now())
	if err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	if err := patch.Apply(ctx, launcher.client, crclient.ApplyConfigurationFromUnstructured(object), FieldOwner, false); err != nil {
		// Two replicas can reach this line for one work item. The loser must
		// report the winner's object rather than a fault, because both are
		// executing the same authorised attempt.
		if faults.IsCode(err, faults.CodeAlreadyExists) || faults.IsCode(err, faults.CodeConflict) {
			raced, rediscovered, getErr := launcher.get(ctx, key)
			if getErr != nil {
				return orchestration.LaunchOutcome{}, getErr
			}
			if rediscovered {
				return launcher.existing(ctx, envelope, key, raced)
			}
		}
		return orchestration.LaunchOutcome{}, err
	}
	// The object was just created suspended, and Kueue has not seen it yet, so
	// its state is Created by construction. Re-reading to discover that would
	// cost a round trip for an answer this code already knows.
	return orchestration.LaunchOutcome{ExternalID: key.Name, Existed: false, State: orchestration.AttemptCreated}, nil
}

// existing reports a live object as an already-launched workload.
func (launcher *Launcher) existing(ctx context.Context, envelope orchestration.WorkloadEnvelope, key types.NamespacedName, live *unstructured.Unstructured) (orchestration.LaunchOutcome, error) {
	if err := checkFence(envelope, live); err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	projection, err := launcher.project(ctx, live)
	if err != nil {
		return orchestration.LaunchOutcome{}, err
	}
	return orchestration.LaunchOutcome{ExternalID: key.Name, Existed: true, State: projection.State}, nil
}

// Observe reads the current state of a previously applied JobSet.
func (launcher *Launcher) Observe(ctx context.Context, envelope orchestration.WorkloadEnvelope) (orchestration.Observation, error) {
	if err := launcher.admit(ctx, envelope); err != nil {
		return orchestration.Observation{}, err
	}
	key, err := launcher.key(envelope)
	if err != nil {
		return orchestration.Observation{}, err
	}
	observedAt := launcher.clock.Now()
	live, found, err := launcher.get(ctx, key)
	if err != nil {
		return orchestration.Observation{}, err
	}
	if !found {
		return orchestration.Observation{}, notFound("jobset_not_found", "no jobset was applied for this envelope")
	}
	if err := checkFence(envelope, live); err != nil {
		return orchestration.Observation{}, err
	}
	projection, err := launcher.project(ctx, live)
	if err != nil {
		return orchestration.Observation{}, err
	}
	return orchestration.Observation{
		ExternalID: key.Name,
		State:      projection.State,
		Sequence:   projection.Sequence,
		Failure:    projection.Failure,
		ObservedAt: observedAt,
	}, nil
}

// Cancel stops the workload by suspending it and recording why.
//
// It suspends and annotates rather than deleting. A deleted JobSet answers
// "was this cancelled, or did it never exist?" with silence, and the next
// duplicate delivery of the work item would apply the object again and start
// the attempt a second time. Suspension leaves the outcome legible to the
// reconciler and to an operator, and the object is reclaimed by its own TTL
// rather than by a race with the queue.
func (launcher *Launcher) Cancel(ctx context.Context, envelope orchestration.WorkloadEnvelope, reason string) error {
	if err := launcher.admit(ctx, envelope); err != nil {
		return err
	}
	if err := validateReason(reason); err != nil {
		return err
	}
	key, err := launcher.key(envelope)
	if err != nil {
		return err
	}
	live, found, err := launcher.get(ctx, key)
	if err != nil {
		return err
	}
	if !found {
		// Already gone. A cancellation that raced a completion, a TTL sweep, or
		// an operator's own deletion arrives exactly like this.
		return nil
	}
	if err := checkFence(envelope, live); err != nil {
		return err
	}
	projection, err := launcher.project(ctx, live)
	if err != nil {
		return err
	}
	if projection.State.Terminal() {
		// The attempt already published an outcome. Suspending it now would
		// annotate a finished object with a stop it never obeyed.
		return nil
	}
	if strings.TrimSpace(live.GetAnnotations()[CancelledAtAnnotation]) != "" {
		// The intent is already durable. Rewriting it would replace the first
		// reason -- the one an operator will read -- with whichever replay
		// happened to arrive last.
		return nil
	}
	before := live.DeepCopy()
	if _, err := metadata.Apply(live, nil, map[string]string{
		CancelledAtAnnotation:        launcher.clock.Now().UTC().Format(time.RFC3339Nano),
		CancellationReasonAnnotation: reason,
	}); err != nil {
		return err
	}
	if err := unstructured.SetNestedField(live.Object, true, "spec", "suspend"); err != nil {
		return invalid("jobset_suspend_unwritable", "the jobset suspend field could not be set", err)
	}
	return patch.Object(ctx, launcher.client, before, live)
}

// admit is the guard every method shares: a usable context and an envelope
// whose execution ticket is active right now.
func (launcher *Launcher) admit(ctx context.Context, envelope orchestration.WorkloadEnvelope) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return canceled(err)
	}
	return envelope.Validate(launcher.clock.Now())
}

// key resolves the namespace/name a workload's object lives at. Both halves are
// derived from the envelope, which is what makes launch idempotent.
func (launcher *Launcher) key(envelope orchestration.WorkloadEnvelope) (types.NamespacedName, error) {
	domain, err := Domain(envelope)
	if err != nil {
		return types.NamespacedName{}, err
	}
	return types.NamespacedName{Namespace: domain.Namespace(), Name: ObjectName(envelope)}, nil
}

// get reads one JobSet, reporting a missing object as absent rather than as a
// fault. Every caller here has to distinguish "nothing applied yet" from "the
// API server refused", and collapsing them would make a permissions failure
// look like a workload that was never started.
func (launcher *Launcher) get(ctx context.Context, key types.NamespacedName) (*unstructured.Unstructured, bool, error) {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(JobSetGVK)
	if err := launcher.client.Get(ctx, key, object); err != nil {
		qualified := kubernetes.QualifyObject(ctx, err, operation+".Get", reference(key), nil)
		if faults.IsCode(qualified, faults.CodeNotFound) {
			return nil, false, nil
		}
		return nil, false, qualified
	}
	if object.GetKind() != JobSetGVK.Kind || object.GetAPIVersion() != GroupVersion.String() {
		return nil, false, failedPrecondition("object_kind_mismatch", "the client returned an object of a different kind")
	}
	return object, true, nil
}

// project reads the live object, refined by Kueue when an admission reader is
// wired.
func (launcher *Launcher) project(ctx context.Context, live *unstructured.Unstructured) (Projection, error) {
	admission := Admission{}
	if !nilInterface(launcher.admissions) {
		found, err := launcher.admissions.Admission(ctx, live.GetNamespace(), live.GetUID())
		if err != nil {
			return Projection{}, err
		}
		admission = found
	}
	return Project(live, admission)
}

// checkFence refuses a request from a replica that has been superseded.
//
// The comparison is one-sided on purpose. A caller holding a newer fence is the
// current owner and may address the object; a caller holding an older one lost
// its claim, and letting it cancel or re-launch is exactly what fencing exists
// to stop.
func checkFence(envelope orchestration.WorkloadEnvelope, object *unstructured.Unstructured) error {
	recorded, err := Fence(object)
	if err != nil {
		return err
	}
	if envelope.ExecutionTicket.Claims.FencingToken < recorded {
		return conflict("stale_fencing_token", "the request carries a fencing token an existing owner has superseded")
	}
	return nil
}

func reference(key types.NamespacedName) kubernetes.ObjectReference {
	return kubernetes.ObjectReference{
		APIVersion: GroupVersion.String(),
		Kind:       JobSetGVK.Kind,
		Namespace:  key.Namespace,
		Name:       key.Name,
	}
}

func validateReason(reason string) error {
	if reason == "" || len(reason) > orchestration.MaximumReasonLength || reason != strings.TrimSpace(reason) {
		return invalid("reason_invalid", "cancellation reason is empty, oversized, or not trimmed", nil)
	}
	return nil
}

func terminalFailure(reason, message string, object *unstructured.Unstructured) error {
	return faults.New(faults.CodeInternal, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithFields(kubernetes.ReferenceFor(object, JobSetGVK).Fields()),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func cancellationFailure(reason string, object *unstructured.Unstructured) error {
	fields := kubernetes.ReferenceFor(object, JobSetGVK).Fields()
	if fields == nil {
		fields = faults.Fields{}
	}
	if strings.TrimSpace(reason) != "" {
		fields["cancellation_reason"] = reason
	}
	return faults.New(faults.CodeCanceled, "the workload was cancelled",
		faults.WithReason("jobset_cancelled"), faults.WithOperation(operation),
		faults.WithFields(fields), faults.WithRetryPolicy(faults.NoRetry()))
}

func canceled(cause error) error {
	return faults.Wrap(cause, faults.CodeCanceled, "kubernetes launcher request was cancelled",
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

// denied mirrors the API server's own answer to an object that violates a
// ValidatingAdmissionPolicy. Raising the same code here means the refusal is
// classified identically whether this package caught it or the cluster did.
func denied(reason, message string) error {
	return faults.New(faults.CodePermissionDenied, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()))
}

// unavailable is the only helper here that admits a retry: an API server that
// is unreachable now may answer on the next attempt, whereas every other
// refusal in this package is a decision replaying cannot change.
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
// not == nil, so a launcher handed a (*fakeClient)(nil) would pass a plain
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

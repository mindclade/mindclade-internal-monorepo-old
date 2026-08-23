// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package kubernetes

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/scheduling"
	schedulingjobset "go.mindclade.dev/control/scheduling/adapters/jobset"
	schedulingkueue "go.mindclade.dev/control/scheduling/adapters/kueue"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/kubernetes/metadata"
)

// GroupName and Version pin the JobSet API the cluster's admission policies
// name. The block-job-activation and capacity-queue policies both match
// jobset.x-k8s.io/v1alpha2 explicitly, so an object built for another version
// would bypass the rules that keep batch work suspended and in its own queue.
const (
	GroupName = schedulingjobset.GroupName
	Version   = schedulingjobset.Version
)

// GroupVersion and JobSetGVK are the only kind this adapter builds or reads.
var (
	GroupVersion = schedulingjobset.GroupVersion
	JobSetGVK    = schedulingjobset.JobSetGVK
)

// JobSet condition types.
//
// They are spelled out rather than imported because sigs.k8s.io/jobset is not a
// module dependency. Completion is read from these conditions and not from the
// upstream per-replicated-job terminal counters: infra/kubernetes/MLOPS.md
// records that those counters are not treated as a reliable windowed outcome
// signal, so a stage that trusted them could conclude a run finished on a
// counter that had not settled.
const (
	ConditionCompleted = "Completed"
	ConditionFailed    = "Failed"
	ConditionSuspended = "Suspended"
)

// Annotations this adapter owns on the JobSet.
const (
	// WorkloadIDAnnotation, AttemptAnnotation, and TicketIDAnnotation make a
	// live object traceable back to the attempt that authorised it, rather than
	// to an attempt that merely looks like it.
	WorkloadIDAnnotation = "mindclade.dev/workload-id"
	AttemptAnnotation    = "mindclade.dev/attempt"
	TicketIDAnnotation   = "mindclade.dev/execution-ticket"
	// ConfigDigestAnnotation records the resolved configuration the attempt was
	// compiled from.
	ConfigDigestAnnotation = "mindclade.dev/resolved-config-digest"
	// FencingTokenAnnotation is the fence. Kubernetes has no other place to put
	// it: an object is not a lease, so without a recorded fence a replaced
	// replica could not tell its own JobSet from its successor's.
	FencingTokenAnnotation = "mindclade.dev/fencing-token"
	// CancelledAtAnnotation and CancellationReasonAnnotation are the durable
	// record that a stop was requested. They are why Cancel suspends and
	// annotates rather than deleting: a deleted object answers "was this
	// cancelled or did it never exist?" with silence, and the next duplicate
	// delivery of the work item would start the attempt again.
	CancelledAtAnnotation        = "mindclade.dev/cancelled-at"
	CancellationReasonAnnotation = "mindclade.dev/cancellation-reason"
)

// ManagedBy and ComponentName identify this adapter's objects.
const (
	ManagedBy     = "mindclade-orchestration"
	ComponentName = "orchestration-workload"
)

const (
	// ObjectNamePrefix marks a JobSet this control plane owns and keeps the
	// name a valid DNS label, which must not begin with a digit.
	ObjectNamePrefix = "mc-"
	// objectNameDigestLength carries 128 bits of attempt identity while leaving
	// the name far inside the 63-character DNS label bound.
	objectNameDigestLength = 32
	// ReplicatedJobName is the single replicated job every workload builds.
	// JobSet requires the name; nothing about a Mindclade attempt varies it.
	ReplicatedJobName = "worker"
	// ContainerName is the single container name.
	ContainerName = "worker"
)

// imageDigestPattern and PlaceholderDigest mirror the cluster's own admission
// expression. The restricted-pods policy requires an immutable sha256 digest
// AND separately denies the all-zero placeholder that scaffolded manifests
// carry, so both halves are re-checked here: a floating tag would let two
// attempts of one stage run different code under the same resolved-config
// digest, and the placeholder is a manifest nobody has finished writing.
var imageDigestPattern = regexp.MustCompile(`^[^@]+@sha256:[0-9a-f]{64}$`)

// PlaceholderDigest is the scaffold digest the cluster denies by name.
const PlaceholderDigest = "@sha256:0000000000000000000000000000000000000000000000000000000000000000"

// MaximumParsedEntries bounds every list this adapter walks out of an
// unstructured object. A cluster object is untrusted input to a control-plane
// process the same way a request body is; an object with a million conditions
// must fail, not allocate.
const MaximumParsedEntries = 64

// stateRankSpan lets one observation sequence carry both the object generation
// and the lifecycle position, with the generation dominating.
const stateRankSpan = uint64(16)

// TopologyRequirement is the topology constraint carried on the pod template.
//
// It is one choice rather than two independent fields because Kueue treats the
// required and preferred annotations as mutually exclusive, and emitting both
// would leave the effective behaviour dependent on which one its webhook read
// first. It is an annotation and not a spec field: there is no JobSet or Job
// API surface for topology-aware scheduling.
type TopologyRequirement struct {
	// Level is the node label Kueue groups on, such as
	// topology.kubernetes.io/zone. Empty means no topology constraint.
	Level string
	// Required makes the constraint mandatory rather than best effort.
	Required bool
}

// PodSpec is the half of a workload that orchestration does not decide: what
// image to run, as whom, and with what entry point.
//
// Resources are absent on purpose. They come from the execution ticket, so a
// caller cannot ask for one thing and be charged for another.
type PodSpec struct {
	ServiceAccountName string
	Image              string
	Command            []string
	Args               []string
	Env                []corev1.EnvVar
	Replicas           int32
	// Devices is the GPU count. It must agree with the ticket's GPU memory
	// budget; Requirements enforces that.
	Devices  uint32
	Topology TopologyRequirement
}

// Resolver turns an envelope into the pod specification that runs it.
// Resolution is policy -- which image, which service account, which entry
// point -- and policy does not belong in an adapter.
type Resolver interface {
	Resolve(orchestration.WorkloadEnvelope) (PodSpec, error)
}

// ObjectName is the deterministic JobSet name for one envelope.
//
// It is a digest of the attempt's identity rather than a readable composition
// of its fields, because a DNS label is 63 characters and a truncated readable
// name would collide between two attempts of the same stage. Determinism is
// what makes launch idempotent: a redelivered work item addresses the object
// the first delivery created instead of creating a second one.
func ObjectName(envelope orchestration.WorkloadEnvelope) string {
	preimage := strings.Join([]string{
		envelope.WorkloadID,
		envelope.RunID,
		envelope.JobID,
		envelope.StageID,
		strconv.FormatUint(uint64(envelope.Attempt), 10),
	}, "\x1f")
	return ObjectNamePrefix + identifiers.SHA256String(preimage).Hex()[:objectNameDigestLength]
}

// Build assembles the suspended JobSet for one envelope.
//
// Suspend is always true. Kueue admits by unsuspending, and the cluster denies
// an unsuspended batch object in a namespace whose activation is blocked, so an
// object built any other way is refused by the API server rather than queued.
func Build(envelope orchestration.WorkloadEnvelope, spec PodSpec, now time.Time) (*unstructured.Unstructured, error) {
	if err := envelope.Validate(now); err != nil {
		return nil, err
	}
	domain, err := Domain(envelope)
	if err != nil {
		return nil, err
	}
	if spec.Replicas <= 0 {
		return nil, invalid("replicas_invalid", "a workload must declare at least one replica", nil)
	}
	requirements, err := Requirements(envelope.ExecutionTicket.Claims.Budget, spec.Devices)
	if err != nil {
		return nil, err
	}
	template, err := podTemplate(envelope, spec, domain, requirements)
	if err != nil {
		return nil, err
	}
	labels, err := ObjectLabels(domain)
	if err != nil {
		return nil, err
	}
	parallelism := int32(1)
	completions := int32(1)
	backoff := int32(0)
	suspend := true
	object := schedulingjobset.JobSet{
		TypeMeta: metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: JobSetGVK.Kind},
		ObjectMeta: metav1.ObjectMeta{
			Name:        ObjectName(envelope),
			Namespace:   domain.Namespace(),
			Labels:      labels,
			Annotations: IdentityAnnotations(envelope),
		},
		Spec: schedulingjobset.JobSetSpec{
			Suspend: &suspend,
			ReplicatedJobs: []schedulingjobset.ReplicatedJob{{
				Name:     ReplicatedJobName,
				Replicas: spec.Replicas,
				Template: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						Parallelism: &parallelism,
						Completions: &completions,
						// Retries are orchestration's attempt budget, not the
						// Job controller's. A Job that retried on its own would
						// consume reserved capacity for an attempt no execution
						// ticket authorised, and would report its outcome under
						// a fence that had already been superseded.
						BackoffLimit: &backoff,
						Template:     template,
					},
				},
			}},
		},
	}
	rendered, err := schedulingjobset.ToUnstructured(object)
	if err != nil {
		return nil, err
	}
	if err := Verify(rendered); err != nil {
		return nil, err
	}
	return rendered, nil
}

// ObjectLabels are the labels the capacity-queue admission policy denies on,
// plus this adapter's ownership labels.
func ObjectLabels(domain scheduling.CapacityDomain) (map[string]string, error) {
	managed, err := metadata.ManagedLabels(ManagedBy, ComponentName, "", "")
	if err != nil {
		return nil, err
	}
	labels := metadata.Merge(managed, map[string]string{
		schedulingkueue.QueueNameLabel:     domain.QueueName(),
		schedulingkueue.WorkloadClassLabel: string(domain.WorkloadClass()),
	})
	if err := metadata.ValidateLabels(labels); err != nil {
		return nil, err
	}
	return labels, nil
}

// IdentityAnnotations records which attempt an object belongs to and under
// which fence.
func IdentityAnnotations(envelope orchestration.WorkloadEnvelope) map[string]string {
	return map[string]string{
		WorkloadIDAnnotation:   envelope.WorkloadID,
		AttemptAnnotation:      strconv.FormatUint(uint64(envelope.Attempt), 10),
		TicketIDAnnotation:     envelope.ExecutionTicket.Claims.TicketID,
		ConfigDigestAnnotation: envelope.ResolvedConfigDigest.String(),
		FencingTokenAnnotation: strconv.FormatUint(envelope.ExecutionTicket.Claims.FencingToken, 10),
	}
}

// podTemplate builds the pod template, satisfying every condition the
// restricted-pods policy denies on.
func podTemplate(envelope orchestration.WorkloadEnvelope, spec PodSpec, domain scheduling.CapacityDomain, requirements corev1.ResourceRequirements) (corev1.PodTemplateSpec, error) {
	if strings.TrimSpace(spec.ServiceAccountName) == "" || spec.ServiceAccountName == "default" {
		return corev1.PodTemplateSpec{}, denied("service_account_invalid", "workloads must use a dedicated, non-default ServiceAccount")
	}
	if !imageDigestPattern.MatchString(spec.Image) {
		return corev1.PodTemplateSpec{}, denied("image_not_digest_pinned", "container image must use an immutable sha256 digest")
	}
	if strings.HasSuffix(spec.Image, PlaceholderDigest) {
		return corev1.PodTemplateSpec{}, denied("image_placeholder_digest", "container image uses the scaffold placeholder digest")
	}
	labels, err := ObjectLabels(domain)
	if err != nil {
		return corev1.PodTemplateSpec{}, err
	}
	annotations, err := TopologyAnnotations(spec.Topology, domain)
	if err != nil {
		return corev1.PodTemplateSpec{}, err
	}
	automount := false
	nonRoot := true
	privileged := false
	escalation := false
	readOnlyRoot := true
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: annotations},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyNever,
			ServiceAccountName:           spec.ServiceAccountName,
			AutomountServiceAccountToken: &automount,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   &nonRoot,
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name:      ContainerName,
				Image:     spec.Image,
				Command:   append([]string(nil), spec.Command...),
				Args:      append([]string(nil), spec.Args...),
				Env:       append([]corev1.EnvVar(nil), spec.Env...),
				Resources: requirements,
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: &escalation,
					Privileged:               &privileged,
					ReadOnlyRootFilesystem:   &readOnlyRoot,
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
			// The workload deadline is enforced by the Job controller as well
			// as by orchestration, because a control plane that lost its lease
			// stops enforcing anything and the pod would otherwise outlive the
			// ticket that authorised it.
			ActiveDeadlineSeconds: activeDeadline(envelope),
		},
	}, nil
}

// activeDeadline is the ticket window expressed for the Job controller. A
// deadline in the past is not encoded at all: the envelope validation above
// already refused it, so reaching here with one would be a bug rather than a
// value to clamp.
func activeDeadline(envelope orchestration.WorkloadEnvelope) *int64 {
	seconds := int64(envelope.Deadline.Sub(envelope.CreatedAt).Seconds())
	if seconds <= 0 {
		return nil
	}
	return &seconds
}

// TopologyAnnotations renders the pod-template topology constraint.
//
// At most one annotation is produced, and a constraint on a non-accelerator
// domain is refused: the batch-cpu flavor declares no topologyName, so a
// topology-constrained CPU placement is not merely unsatisfiable, it is a
// request Kueue has no way to interpret.
func TopologyAnnotations(requirement TopologyRequirement, domain scheduling.CapacityDomain) (map[string]string, error) {
	level := strings.TrimSpace(requirement.Level)
	if level == "" {
		return map[string]string{}, nil
	}
	if !domain.Flavor().TopologyAware() {
		return nil, failedPrecondition("topology_unsupported", "the capacity domain's flavor declares no topology")
	}
	key := schedulingkueue.PreferredTopologyAnnotation
	if requirement.Required {
		key = schedulingkueue.RequiredTopologyAnnotation
	}
	annotations := map[string]string{key: level}
	if err := metadata.ValidateAnnotations(annotations); err != nil {
		return nil, err
	}
	return annotations, nil
}

// Verify re-applies the cluster's admission conditions to an object this
// package produced or read back.
//
// It duplicates rules the API server also enforces, on purpose. The API server
// is the authority, but it is reached asynchronously and its denial arrives
// attached to an object rather than to the run that wanted it. Failing here
// turns "a workload silently never started" into "this launch was refused, and
// here is the condition it failed". The pod and container conditions are
// delegated to control/scheduling/adapters/jobset rather than restated, because
// two copies of an eleven-clause policy drift.
func Verify(object *unstructured.Unstructured) error {
	if object == nil {
		return invalid("object_nil", "jobset object is required", nil)
	}
	if err := schedulingjobset.VerifyJobSet(object); err != nil {
		return err
	}
	annotations := object.GetAnnotations()
	for _, key := range []string{WorkloadIDAnnotation, AttemptAnnotation, TicketIDAnnotation, FencingTokenAnnotation} {
		if strings.TrimSpace(annotations[key]) == "" {
			return failedPrecondition("identity_annotation_missing", "the jobset does not record "+key)
		}
	}
	if _, err := strconv.ParseUint(annotations[FencingTokenAnnotation], 10, 64); err != nil {
		return invalid("fencing_token_invalid", "the jobset records an unparseable fencing token", err)
	}
	// The delegated pod verification checks digest pinning but not the
	// placeholder, which the cluster denies separately and by name. Indexing is
	// safe here: VerifyJobSet already established exactly one replicated job
	// with exactly one container.
	decoded, err := schedulingjobset.FromUnstructured(object)
	if err != nil {
		return err
	}
	image := decoded.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.Containers[0].Image
	if strings.HasSuffix(image, PlaceholderDigest) {
		return denied("image_placeholder_digest", "container image uses the scaffold placeholder digest")
	}
	return nil
}

// Fence recovers the fencing token an object was created under. A missing
// annotation is refused rather than read as zero: zero would compare as older
// than every live fence and would let any caller claim the object.
func Fence(object *unstructured.Unstructured) (uint64, error) {
	if object == nil {
		return 0, invalid("object_nil", "jobset object is required", nil)
	}
	raw, present := object.GetAnnotations()[FencingTokenAnnotation]
	if !present {
		return 0, failedPrecondition("fencing_token_missing", "the jobset records no fencing token")
	}
	fence, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, invalid("fencing_token_invalid", "the jobset records an unparseable fencing token", err)
	}
	return fence, nil
}

// Projection is one JobSet read as an attempt.
type Projection struct {
	State orchestration.AttemptState
	// Failure explains an unsuccessful terminal state. Its fault code carries
	// the disposition the stage should apply.
	Failure error
	// Sequence orders this observation against others of the same attempt.
	Sequence uint64
}

// Project reads a live JobSet, refined by what Kueue says about its admission.
//
// Order matters here. Terminal conditions win over everything, because an
// object that completed and was then annotated for cancellation still
// completed. Cancellation outranks the suspended/running split, because a
// suspended-and-cancelled object is being stopped rather than waiting for
// quota. Only then does admission distinguish "queued, no quota" from "quota
// granted, controller has not unsuspended yet".
func Project(object *unstructured.Unstructured, admission Admission) (Projection, error) {
	if object == nil {
		return Projection{}, invalid("object_nil", "jobset object is required", nil)
	}
	conditions, err := parseConditions(object)
	if err != nil {
		return Projection{}, err
	}
	sequence, err := sequenceFor(object)
	if err != nil {
		return Projection{}, err
	}
	state, failure := classify(object, conditions, admission)
	return Projection{State: state, Failure: failure, Sequence: sequence*stateRankSpan + stateRank(state)}, nil
}

func classify(object *unstructured.Unstructured, conditions map[string]bool, admission Admission) (orchestration.AttemptState, error) {
	annotations := object.GetAnnotations()
	switch {
	case conditions[ConditionFailed]:
		return orchestration.AttemptFailed, terminalFailure("jobset_failed", "the jobset reported a failed condition", object)
	case conditions[ConditionCompleted]:
		return orchestration.AttemptCompleted, nil
	}
	cancelled := strings.TrimSpace(annotations[CancelledAtAnnotation]) != ""
	if object.GetDeletionTimestamp() != nil || cancelled {
		// The object is only cancelled once the controller has acknowledged
		// the suspension. Reporting cancelled while pods are still terminating
		// would let the stage publish an outcome for work that is still
		// writing.
		if conditions[ConditionSuspended] {
			return orchestration.AttemptCancelled, cancellationFailure(annotations[CancellationReasonAnnotation], object)
		}
		return orchestration.AttemptCancelling, nil
	}
	if suspended(object) {
		if admission.Placed() {
			return orchestration.AttemptStarting, nil
		}
		return orchestration.AttemptCreated, nil
	}
	return orchestration.AttemptRunning, nil
}

func suspended(object *unstructured.Unstructured) bool {
	value, found, err := unstructured.NestedBool(object.Object, "spec", "suspend")
	if err != nil || !found {
		// An object with no readable suspend field is not running as far as
		// this adapter is concerned. Build never produces one, and guessing
		// "running" would report an attempt in flight that never started.
		return true
	}
	return value
}

// sequenceFor reads the object generation.
//
// Generation is monotone and advances only on a spec change, which is exactly
// the granularity an observation ordering needs. resourceVersion is explicitly
// documented as opaque and must not be compared, so it cannot be used here even
// though etcd happens to make it look numeric.
func sequenceFor(object *unstructured.Unstructured) (uint64, error) {
	generation := object.GetGeneration()
	if generation <= 0 {
		return 0, failedPrecondition("object_generation_missing", "the jobset reports no generation")
	}
	if uint64(generation) > (^uint64(0))/stateRankSpan {
		return 0, failedPrecondition("object_generation_out_of_range", "the jobset generation is beyond the adapter bound")
	}
	return uint64(generation), nil
}

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
		return 10
	}
}

// parseConditions reads status.conditions as a type -> is-true map.
func parseConditions(object *unstructured.Unstructured) (map[string]bool, error) {
	entries, found, err := unstructured.NestedSlice(object.Object, "status", "conditions")
	if err != nil {
		return nil, invalid("jobset_conditions_invalid", "jobset conditions are malformed", err)
	}
	conditions := make(map[string]bool, len(entries))
	if !found {
		return conditions, nil
	}
	if len(entries) > MaximumParsedEntries {
		return nil, failedPrecondition("jobset_condition_bound", "the jobset declares more conditions than the parser bound")
	}
	for _, entry := range entries {
		fields, ok := entry.(map[string]any)
		if !ok {
			return nil, invalid("jobset_condition_invalid", "a jobset condition entry is malformed", nil)
		}
		conditionType, foundType, err := unstructured.NestedString(fields, "type")
		if err != nil || !foundType {
			return nil, invalid("jobset_condition_type_missing", "a jobset condition has no type", err)
		}
		status, _, err := unstructured.NestedString(fields, "status")
		if err != nil {
			return nil, invalid("jobset_condition_status_invalid", "a jobset condition status is malformed", err)
		}
		conditions[conditionType] = status == string(metav1.ConditionTrue)
	}
	return conditions, nil
}

// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package kubernetes

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/control/orchestration/launchertest"
	"go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/control/scheduling"
	schedulingkueue "go.mindclade.dev/control/scheduling/adapters/kueue"
	"go.mindclade.dev/libs/go/faults"
)

// pinnedImage satisfies the cluster's non-placeholder digest rule. The all-zero
// digest is denied by name, so the fixture uses a different one.
const pinnedImage = "registry.invalid/mindclade/worker@sha256:1111111111111111111111111111111111111111111111111111111111111111"

type fixedResolver struct {
	spec PodSpec
	err  error
}

func (resolver fixedResolver) Resolve(orchestration.WorkloadEnvelope) (PodSpec, error) {
	if resolver.err != nil {
		return PodSpec{}, resolver.err
	}
	return resolver.spec, nil
}

func validPodSpec() PodSpec {
	return PodSpec{
		ServiceAccountName: "mindclade-worker",
		Image:              pinnedImage,
		Command:            []string{"/usr/local/bin/worker"},
		Replicas:           1,
	}
}

// testScheme registers the two unstructured kinds the fake client serves. The
// real client resolves them through discovery; the fake needs them declared.
func testScheme(t testing.TB) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(JobSetGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(GroupVersion.WithKind("JobSetList"), &unstructured.UnstructuredList{})
	scheme.AddKnownTypeWithName(schedulingkueue.WorkloadGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(WorkloadListGVK, &unstructured.UnstructuredList{})
	return scheme
}

// serverManaged supplies the metadata a real API server assigns and the fake
// client does not.
//
// metadata.generation and metadata.uid are server-owned: nothing a client sends
// can set them, and the fake simply leaves them empty. The launcher reads both
// -- generation to order observations, uid to find the Kueue Workload -- so
// without this the tests would be exercising a client that is missing fields
// every real cluster provides, and the only way to make them pass would be to
// weaken the adapter against a limitation of the double.
type serverManaged struct {
	crclient.Client
}

func (client serverManaged) Get(ctx context.Context, key crclient.ObjectKey, object crclient.Object, options ...crclient.GetOption) error {
	if err := client.Client.Get(ctx, key, object, options...); err != nil {
		return err
	}
	if object.GetGeneration() == 0 {
		object.SetGeneration(1)
	}
	if object.GetUID() == "" {
		object.SetUID(types.UID("uid-" + object.GetNamespace() + "-" + object.GetName()))
	}
	return nil
}

func newFakeClient(t testing.TB, objects ...crclient.Object) crclient.Client {
	t.Helper()
	return serverManaged{Client: fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(objects...).Build()}
}

func TestConformance(t *testing.T) {
	launchertest.Conformance(t, func(tb testing.TB) orchestration.Launcher {
		tb.Helper()
		launcher, err := New(newFakeClient(tb), fixedResolver{spec: validPodSpec()})
		if err != nil {
			tb.Fatalf("New: %v", err)
		}
		return launcher
	})
}

func newLauncher(t *testing.T, client crclient.Client, resolver Resolver, options ...Option) *Launcher {
	t.Helper()
	launcher, err := New(client, resolver, options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return launcher
}

func TestNewRejectsMissingCollaborators(t *testing.T) {
	if _, err := New(nil, fixedResolver{}); !faults.IsCode(err, faults.CodeUnavailable) {
		t.Fatalf("New(nil client) = %v, want unavailable", err)
	}
	if _, err := New(newFakeClient(t), nil); !faults.IsCode(err, faults.CodeUnavailable) {
		t.Fatalf("New(nil resolver) = %v, want unavailable", err)
	}
	if _, err := New(newFakeClient(t), fixedResolver{}, WithClock(nil)); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("WithClock(nil) = %v, want invalid_argument", err)
	}
	if _, err := New(newFakeClient(t), fixedResolver{}, WithAdmissions(nil)); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("WithAdmissions(nil) = %v, want invalid_argument", err)
	}
}

// TestLaunchedObjectSatisfiesClusterAdmission is the check that matters most
// here: every condition below is a ValidatingAdmissionPolicy with Deny, so an
// object failing one of them is refused by the API server asynchronously, with
// the failure attached to an object instead of to the run that wanted it.
func TestLaunchedObjectSatisfiesClusterAdmission(t *testing.T) {
	client := newFakeClient(t)
	launcher := newLauncher(t, client, fixedResolver{spec: validPodSpec()})
	envelope := launchertest.Envelope(t, time.Now())
	outcome, err := launcher.Launch(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	live := readJobSet(t, client, "mindclade-batch-cpu", outcome.ExternalID)

	if err := Verify(live); err != nil {
		t.Fatalf("the applied object does not satisfy cluster admission: %v", err)
	}
	if !suspended(live) {
		t.Fatal("the applied jobset is not suspended")
	}
	labels := live.GetLabels()
	if labels[schedulingkueue.QueueNameLabel] != "mindclade-batch-cpu" ||
		labels[schedulingkueue.WorkloadClassLabel] != "batch-cpu" ||
		live.GetNamespace() != "mindclade-batch-cpu" {
		t.Fatalf("the applied object does not form one capacity triple: namespace=%q labels=%v", live.GetNamespace(), labels)
	}
	annotations := live.GetAnnotations()
	if annotations[WorkloadIDAnnotation] != envelope.WorkloadID ||
		annotations[TicketIDAnnotation] != envelope.ExecutionTicket.Claims.TicketID {
		t.Fatalf("the applied object does not record its attempt identity: %v", annotations)
	}
	fence, err := Fence(live)
	if err != nil {
		t.Fatalf("Fence: %v", err)
	}
	if fence != launchertest.DefaultFencingToken {
		t.Fatalf("recorded fence = %d, want %d", fence, launchertest.DefaultFencingToken)
	}
	// Topology is a pod-template annotation, never a spec field. An empty
	// requirement must produce neither annotation rather than an empty one.
	template, _, err := unstructured.NestedMap(live.Object, "spec", "replicatedJobs")
	if err == nil && template != nil {
		t.Fatal("replicatedJobs decoded as a map; the object shape changed")
	}
}

func readJobSet(t *testing.T, client crclient.Client, namespace, name string) *unstructured.Unstructured {
	t.Helper()
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(JobSetGVK)
	if err := client.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, object); err != nil {
		t.Fatalf("get %s/%s: %v", namespace, name, err)
	}
	return object
}

// TestCancelSuspendsAndRecordsRatherThanDeleting pins the decision that keeps a
// redelivered work item from restarting a cancelled attempt.
func TestCancelSuspendsAndRecordsRatherThanDeleting(t *testing.T) {
	client := newFakeClient(t)
	launcher := newLauncher(t, client, fixedResolver{spec: validPodSpec()})
	envelope := launchertest.Envelope(t, time.Now())
	outcome, err := launcher.Launch(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := launcher.Cancel(context.Background(), envelope, "operator stop"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	live := readJobSet(t, client, "mindclade-batch-cpu", outcome.ExternalID)
	if !suspended(live) {
		t.Fatal("a cancelled jobset is not suspended")
	}
	annotations := live.GetAnnotations()
	if annotations[CancellationReasonAnnotation] != "operator stop" || annotations[CancelledAtAnnotation] == "" {
		t.Fatalf("cancellation was not recorded: %v", annotations)
	}
	// A replayed cancellation must not overwrite the first reason, which is the
	// one an operator reads.
	if err := launcher.Cancel(context.Background(), envelope, "different reason"); err != nil {
		t.Fatalf("replayed cancel: %v", err)
	}
	live = readJobSet(t, client, "mindclade-batch-cpu", outcome.ExternalID)
	if live.GetAnnotations()[CancellationReasonAnnotation] != "operator stop" {
		t.Fatalf("a replayed cancellation rewrote the reason: %v", live.GetAnnotations())
	}
}

func TestResolverFailureStopsTheApply(t *testing.T) {
	client := newFakeClient(t)
	launcher := newLauncher(t, client, fixedResolver{err: invalid("policy_missing", "no pod policy", nil)})
	envelope := launchertest.Envelope(t, time.Now())
	if _, err := launcher.Launch(context.Background(), envelope); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("resolver failure = %v, want invalid_argument", err)
	}
	if _, err := launcher.Observe(context.Background(), envelope); !faults.IsCode(err, faults.CodeNotFound) {
		t.Fatalf("observe after a refused launch = %v, want not_found", err)
	}
}

// TestNonCompliantPodSpecIsRefusedLocally proves the local re-check earns its
// keep: each of these is denied by the cluster, and catching it here attaches
// the refusal to the launch instead of to an orphaned object.
func TestNonCompliantPodSpecIsRefusedLocally(t *testing.T) {
	envelope := launchertest.Envelope(t, time.Now())
	now := time.Now()
	cases := map[string]func(*PodSpec){
		"default service account":  func(spec *PodSpec) { spec.ServiceAccountName = "default" },
		"missing service account":  func(spec *PodSpec) { spec.ServiceAccountName = "" },
		"floating image tag":       func(spec *PodSpec) { spec.Image = "registry.invalid/mindclade/worker:latest" },
		"placeholder image digest": func(spec *PodSpec) { spec.Image = strings.ReplaceAll(pinnedImage, "1", "0") },
		"no replicas":              func(spec *PodSpec) { spec.Replicas = 0 },
	}
	for name, mutate := range cases {
		spec := validPodSpec()
		mutate(&spec)
		if _, err := Build(envelope, spec, now); err == nil {
			t.Fatalf("%s was accepted", name)
		} else if orchestration.Classify(err) != orchestration.DispositionTerminal {
			t.Fatalf("%s classified as %q, want terminal", name, orchestration.Classify(err))
		}
	}
}

func TestRequirementsProjectsTheTicketBudget(t *testing.T) {
	budget := runtime_authority.ExecutionBudget{
		CPUMillis:              2500,
		ResidentMemoryBytes:    8 << 30,
		LocalDiskBytes:         1 << 30,
		CheckpointStagingBytes: 1 << 30,
		TelemetrySpoolBytes:    1 << 20,
		OpenFileDescriptors:    1024,
		CPUWorkerThreads:       8,
	}
	requirements, err := Requirements(budget, 0)
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	cpu := requirements.Requests[corev1.ResourceCPU]
	if cpu.MilliValue() != 2500 {
		t.Fatalf("cpu = %s, want 2500m", cpu.String())
	}
	memory := requirements.Requests[corev1.ResourceMemory]
	if memory.Value() != 8<<30 {
		t.Fatalf("memory = %s", memory.String())
	}
	ephemeral := requirements.Requests[corev1.ResourceEphemeralStorage]
	if ephemeral.Value() != (1<<30)+(1<<30)+(1<<20) {
		t.Fatalf("ephemeral storage = %s; the local-disk budgets were not summed", ephemeral.String())
	}
	if _, present := requirements.Requests[ResourceGPU]; present {
		t.Fatal("a cpu-only pod requested an extended gpu resource no cpu node advertises")
	}
	// Kueue charges the request and the cluster requires both, so a divergence
	// would let a workload be admitted for one amount and consume another.
	for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourceEphemeralStorage} {
		request := requirements.Requests[name]
		limit := requirements.Limits[name]
		if request.Cmp(limit) != 0 {
			t.Fatalf("%s request %s does not equal limit %s", name, request.String(), limit.String())
		}
	}

	accelerated := budget
	accelerated.GPUMemoryEstimateBytes = 80 << 30
	withDevices, err := Requirements(accelerated, 8)
	if err != nil {
		t.Fatalf("Requirements with devices: %v", err)
	}
	devices := withDevices.Requests[ResourceGPU]
	if devices.Value() != 8 {
		t.Fatalf("gpu request = %s, want 8", devices.String())
	}
}

func TestRequirementsRefusesIncoherentBudgets(t *testing.T) {
	base := runtime_authority.ExecutionBudget{
		CPUMillis: 1000, ResidentMemoryBytes: 1 << 30, LocalDiskBytes: 1 << 30,
		OpenFileDescriptors: 64, CPUWorkerThreads: 2,
	}
	if _, err := Requirements(base, 4); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("unbudgeted devices = %v, want failed_precondition", err)
	}
	accelerated := base
	accelerated.GPUMemoryEstimateBytes = 1 << 30
	if _, err := Requirements(accelerated, 0); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("budgeted gpu with no devices = %v, want failed_precondition", err)
	}
	// tmpfs pages are charged to the container's memory cgroup, so this pod
	// would be OOM-killed by its own /dev/shm.
	oversharing := base
	oversharing.SharedMemoryBytes = base.ResidentMemoryBytes + 1
	if _, err := Requirements(oversharing, 0); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("shared memory beyond resident = %v, want failed_precondition", err)
	}
	noDisk := base
	noDisk.LocalDiskBytes = 0
	if _, err := Requirements(noDisk, 0); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("ticket granting no local disk = %v, want invalid_argument", err)
	}
	if _, err := Requirements(runtime_authority.ExecutionBudget{}, 0); err == nil {
		t.Fatal("an empty budget was accepted")
	}
}

func TestDomainRefusesAnAcceleratorlessCapacityTriple(t *testing.T) {
	envelope := launchertest.Envelope(t, time.Now())
	domain, err := Domain(envelope)
	if err != nil {
		t.Fatalf("Domain: %v", err)
	}
	if domain.WorkloadClass() != scheduling.WorkloadClassBatchCPU {
		t.Fatalf("domain = %q, want batch-cpu", domain.WorkloadClass())
	}
	// The batch-cpu ClusterQueue does not cover nvidia.com/gpu at all, so this
	// workload could never be admitted.
	accelerated := envelope
	accelerated.ExecutionTicket.Claims.Budget.GPUMemoryEstimateBytes = 1 << 30
	if _, err := Domain(accelerated); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("gpu budget in a cpu domain = %v, want failed_precondition", err)
	}
	unknown := envelope
	unknown.ResourceClass = "training-a100"
	if _, err := Domain(unknown); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("unknown resource class = %v, want invalid_argument", err)
	}
}

func TestTopologyIsAPodTemplateAnnotation(t *testing.T) {
	cpu, err := scheduling.DomainFor(scheduling.WorkloadClassBatchCPU)
	if err != nil {
		t.Fatalf("DomainFor: %v", err)
	}
	gpu, err := scheduling.DomainFor(scheduling.WorkloadClassTrainingH100)
	if err != nil {
		t.Fatalf("DomainFor: %v", err)
	}
	empty, err := TopologyAnnotations(TopologyRequirement{}, gpu)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty requirement = %v, %v", empty, err)
	}
	required, err := TopologyAnnotations(TopologyRequirement{Level: "topology.kubernetes.io/zone", Required: true}, gpu)
	if err != nil {
		t.Fatalf("required topology: %v", err)
	}
	if required[schedulingkueue.RequiredTopologyAnnotation] != "topology.kubernetes.io/zone" {
		t.Fatalf("required annotations = %v", required)
	}
	if _, present := required[schedulingkueue.PreferredTopologyAnnotation]; present {
		t.Fatal("both topology annotations were emitted; kueue treats them as exclusive")
	}
	preferred, err := TopologyAnnotations(TopologyRequirement{Level: "kubernetes.io/hostname"}, gpu)
	if err != nil {
		t.Fatalf("preferred topology: %v", err)
	}
	if preferred[schedulingkueue.PreferredTopologyAnnotation] != "kubernetes.io/hostname" {
		t.Fatalf("preferred annotations = %v", preferred)
	}
	// The cpu flavor declares no topologyName, so a constraint on it is a
	// request kueue has no way to interpret.
	if _, err := TopologyAnnotations(TopologyRequirement{Level: "topology.kubernetes.io/zone"}, cpu); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("topology on a cpu domain = %v, want failed_precondition", err)
	}
}

// jobSetObject builds the minimum live object Project reads.
func jobSetObject(generation int64, suspend bool, conditions map[string]string, annotations map[string]string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{Object: map[string]any{}}
	object.SetGroupVersionKind(JobSetGVK)
	object.SetName("mc-test")
	object.SetNamespace("mindclade-batch-cpu")
	object.SetGeneration(generation)
	object.SetAnnotations(annotations)
	_ = unstructured.SetNestedField(object.Object, suspend, "spec", "suspend")
	entries := make([]any, 0, len(conditions))
	for conditionType, status := range conditions {
		entries = append(entries, map[string]any{"type": conditionType, "status": status})
	}
	if len(entries) > 0 {
		_ = unstructured.SetNestedSlice(object.Object, entries, "status", "conditions")
	}
	return object
}

// TestProjectReadsCompletionFromConditions is the rule MLOPS.md states: the
// upstream per-replicated-job terminal counters are not a reliable windowed
// outcome signal, so completion comes from conditions or not at all.
func TestProjectReadsCompletionFromConditions(t *testing.T) {
	cases := []struct {
		name        string
		object      *unstructured.Unstructured
		admission   Admission
		want        orchestration.AttemptState
		wantFailure bool
	}{
		{
			name:   "suspended without quota is created",
			object: jobSetObject(1, true, nil, nil),
			want:   orchestration.AttemptCreated,
		},
		{
			name:      "suspended with quota is starting",
			object:    jobSetObject(1, true, nil, nil),
			admission: Admission{Known: true, QuotaReserved: true, Admitted: true},
			want:      orchestration.AttemptStarting,
		},
		{
			name:      "evicted quota is not placed",
			object:    jobSetObject(1, true, nil, nil),
			admission: Admission{Known: true, QuotaReserved: true, Admitted: true, Evicted: true},
			want:      orchestration.AttemptCreated,
		},
		{
			name:   "unsuspended is running",
			object: jobSetObject(2, false, nil, nil),
			want:   orchestration.AttemptRunning,
		},
		{
			name:   "completed condition wins",
			object: jobSetObject(2, false, map[string]string{ConditionCompleted: "True"}, nil),
			want:   orchestration.AttemptCompleted,
		},
		{
			name:        "failed condition wins",
			object:      jobSetObject(2, false, map[string]string{ConditionFailed: "True"}, nil),
			want:        orchestration.AttemptFailed,
			wantFailure: true,
		},
		{
			name:   "false conditions are not terminal",
			object: jobSetObject(2, false, map[string]string{ConditionCompleted: "False", ConditionFailed: "False"}, nil),
			want:   orchestration.AttemptRunning,
		},
		{
			name:   "cancellation before the controller acknowledges is cancelling",
			object: jobSetObject(2, true, nil, map[string]string{CancelledAtAnnotation: "2026-08-23T00:00:00Z"}),
			want:   orchestration.AttemptCancelling,
		},
		{
			name: "cancellation the controller acknowledged is cancelled",
			object: jobSetObject(2, true, map[string]string{ConditionSuspended: "True"},
				map[string]string{CancelledAtAnnotation: "2026-08-23T00:00:00Z", CancellationReasonAnnotation: "operator stop"}),
			want:        orchestration.AttemptCancelled,
			wantFailure: true,
		},
		{
			name: "a completed object that was later annotated still completed",
			object: jobSetObject(2, true, map[string]string{ConditionCompleted: "True"},
				map[string]string{CancelledAtAnnotation: "2026-08-23T00:00:00Z"}),
			want: orchestration.AttemptCompleted,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			projection, err := Project(testCase.object, testCase.admission)
			if err != nil {
				t.Fatalf("Project: %v", err)
			}
			if projection.State != testCase.want {
				t.Fatalf("state = %q, want %q", projection.State, testCase.want)
			}
			if (projection.Failure != nil) != testCase.wantFailure {
				t.Fatalf("failure presence = %v, want %v", projection.Failure != nil, testCase.wantFailure)
			}
			if projection.Sequence == 0 {
				t.Fatal("projection carries no observation sequence")
			}
		})
	}
}

// TestSequenceAdvancesWithGenerationAndState is what lets a late observation be
// discarded rather than applied out of order.
func TestSequenceAdvancesWithGenerationAndState(t *testing.T) {
	created, err := Project(jobSetObject(1, true, nil, nil), Admission{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	running, err := Project(jobSetObject(2, false, nil, nil), Admission{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if running.Sequence <= created.Sequence {
		t.Fatalf("sequence did not advance: %d <= %d", running.Sequence, created.Sequence)
	}
	// A state change inside one generation must still advance the sequence, or
	// two different states become indistinguishable to a reconciler.
	cancelling, err := Project(jobSetObject(1, true, nil, map[string]string{CancelledAtAnnotation: "2026-08-23T00:00:00Z"}), Admission{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cancelling.Sequence <= created.Sequence {
		t.Fatalf("state change did not advance the sequence: %d <= %d", cancelling.Sequence, created.Sequence)
	}
	if _, err := Project(jobSetObject(0, true, nil, nil), Admission{}); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("object with no generation = %v, want failed_precondition", err)
	}
}

func TestFenceRequiresAnExplicitAnnotation(t *testing.T) {
	if _, err := Fence(jobSetObject(1, true, nil, nil)); !faults.IsCode(err, faults.CodeFailedPrecondition) {
		t.Fatalf("missing fence = %v, want failed_precondition", err)
	}
	if _, err := Fence(jobSetObject(1, true, nil, map[string]string{FencingTokenAnnotation: "not-a-number"})); !faults.IsCode(err, faults.CodeInvalidArgument) {
		t.Fatalf("unparseable fence = %v, want invalid_argument", err)
	}
	fence, err := Fence(jobSetObject(1, true, nil, map[string]string{FencingTokenAnnotation: " 42 "}))
	if err != nil || fence != 42 {
		t.Fatalf("Fence = %d, %v", fence, err)
	}
}

func TestProjectAdmissionReadsWorkloadConditions(t *testing.T) {
	workload := &unstructured.Unstructured{Object: map[string]any{}}
	workload.SetGroupVersionKind(schedulingkueue.WorkloadGVK)
	workload.SetName("jobset-mc-test-abcde")
	workload.SetNamespace("mindclade-batch-cpu")
	workload.SetLabels(map[string]string{JobUIDLabel: "uid-1"})
	if err := unstructured.SetNestedSlice(workload.Object, []any{
		map[string]any{"type": schedulingkueue.ConditionQuotaReserved, "status": string(metav1.ConditionTrue)},
		map[string]any{"type": schedulingkueue.ConditionAdmitted, "status": string(metav1.ConditionTrue)},
		map[string]any{"type": schedulingkueue.ConditionEvicted, "status": string(metav1.ConditionFalse)},
	}, "status", "conditions"); err != nil {
		t.Fatalf("build workload: %v", err)
	}
	admission, err := ProjectAdmission(workload)
	if err != nil {
		t.Fatalf("ProjectAdmission: %v", err)
	}
	if !admission.Placed() {
		t.Fatalf("admission = %#v, want placed", admission)
	}

	client := newFakeClient(t, workload)
	found, err := KueueAdmissions{Client: client}.Admission(context.Background(), "mindclade-batch-cpu", "uid-1")
	if err != nil {
		t.Fatalf("Admission: %v", err)
	}
	if !found.Placed() {
		t.Fatalf("looked-up admission = %#v, want placed", found)
	}
	// Kueue creates the Workload asynchronously, so the window between apply
	// and admission must read as "nothing known", not as a missing object.
	missing, err := KueueAdmissions{Client: client}.Admission(context.Background(), "mindclade-batch-cpu", "uid-absent")
	if err != nil {
		t.Fatalf("Admission for an unmatched uid: %v", err)
	}
	if missing.Known || missing.Placed() {
		t.Fatalf("unmatched admission = %#v, want unknown", missing)
	}
	if _, err := (KueueAdmissions{}).Admission(context.Background(), "ns", "uid"); !faults.IsCode(err, faults.CodeUnavailable) {
		t.Fatalf("admission without a client = %v, want unavailable", err)
	}
}

// TestAdmissionRefinesTheLaunchedState wires the optional reader and shows the
// distinction it buys: queued for quota versus quota granted.
func TestAdmissionRefinesTheLaunchedState(t *testing.T) {
	client := newFakeClient(t)
	launcher := newLauncher(t, client, fixedResolver{spec: validPodSpec()},
		WithAdmissions(KueueAdmissions{Client: client}))
	envelope := launchertest.Envelope(t, time.Now())
	outcome, err := launcher.Launch(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	observed, err := launcher.Observe(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if observed.State != orchestration.AttemptCreated {
		t.Fatalf("state before admission = %q, want created", observed.State)
	}

	live := readJobSet(t, client, "mindclade-batch-cpu", outcome.ExternalID)
	workload := &unstructured.Unstructured{Object: map[string]any{}}
	workload.SetGroupVersionKind(schedulingkueue.WorkloadGVK)
	workload.SetName("jobset-" + outcome.ExternalID)
	workload.SetNamespace("mindclade-batch-cpu")
	workload.SetLabels(map[string]string{JobUIDLabel: string(live.GetUID())})
	if err := unstructured.SetNestedSlice(workload.Object, []any{
		map[string]any{"type": schedulingkueue.ConditionQuotaReserved, "status": string(metav1.ConditionTrue)},
		map[string]any{"type": schedulingkueue.ConditionAdmitted, "status": string(metav1.ConditionTrue)},
	}, "status", "conditions"); err != nil {
		t.Fatalf("build workload: %v", err)
	}
	if err := client.Create(context.Background(), workload); err != nil {
		t.Fatalf("create workload: %v", err)
	}
	observed, err = launcher.Observe(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Observe after admission: %v", err)
	}
	if observed.State != orchestration.AttemptStarting {
		t.Fatalf("state after admission = %q, want starting", observed.State)
	}
}

func TestObjectNameIsDeterministicAndDNSSafe(t *testing.T) {
	envelope := launchertest.Envelope(t, time.Now())
	name := ObjectName(envelope)
	if name != ObjectName(envelope) {
		t.Fatal("object name is not deterministic")
	}
	if len(name) > 63 {
		t.Fatalf("object name %q is longer than a dns label", name)
	}
	if !strings.HasPrefix(name, ObjectNamePrefix) {
		t.Fatalf("object name %q lacks the ownership prefix", name)
	}
	retry := envelope
	retry.Attempt = 2
	if ObjectName(retry) == name {
		t.Fatal("two attempts share one object name")
	}
	if err := (orchestration.LaunchOutcome{ExternalID: name, State: orchestration.AttemptCreated}).Validate(); err != nil {
		t.Fatalf("object name is not a valid launch handle: %v", err)
	}
}

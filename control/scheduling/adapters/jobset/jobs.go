// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package jobset

import (
	"regexp"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/control/scheduling/adapters/kueue"
)

// MaximumEnvironmentEntries bounds the environment a workload declares.
const MaximumEnvironmentEntries = 128

// objectNamePattern is the DNS label form Kubernetes accepts for these names.
var objectNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// digestPinnedImage mirrors the cluster's own admission expression. Keeping the
// same pattern here means a non-compliant image is rejected at the decision,
// with the run in hand, instead of by an API server with only the object.
var digestPinnedImage = regexp.MustCompile(`^[^@]+@sha256:[0-9a-f]{64}$`)

const placeholderDigest = "@sha256:0000000000000000000000000000000000000000000000000000000000000000"

// JobSet is the minimal typed shape of a jobset.x-k8s.io/v1alpha2 JobSet.
//
// It is declared here rather than imported because sigs.k8s.io/jobset is not a
// module dependency. The shape covers exactly the fields this adapter sets and
// checks; anything else on a live object round-trips untouched through
// unstructured, because Apply is a server-side apply of the fields this package
// owns and not a full replacement.
type JobSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              JobSetSpec `json:"spec,omitempty"`
}

// JobSetSpec is the JobSet spec.
type JobSetSpec struct {
	ReplicatedJobs []ReplicatedJob `json:"replicatedJobs"`
	// Suspend is a pointer because JobSet distinguishes unset from false, and
	// the cluster's block-job-activation policy denies anything that is not
	// explicitly true.
	Suspend *bool `json:"suspend,omitempty"`
}

// ReplicatedJob is one named group of identical Jobs.
type ReplicatedJob struct {
	Name     string                  `json:"name"`
	Replicas int32                   `json:"replicas,omitempty"`
	Template batchv1.JobTemplateSpec `json:"template"`
}

// ContainerSpec is the caller-owned half of a workload: what to run.
type ContainerSpec struct {
	Name    string
	Image   string
	Command []string
	Args    []string
	Env     []corev1.EnvVar
}

// WorkloadSpec is everything about a placement that scheduling policy does not
// decide. Resources, queue, priority, and topology are NOT here: they come from
// the sealed placement, so a caller cannot ask for one thing and be charged for
// another.
type WorkloadSpec struct {
	Name               string
	ReplicatedJobName  string
	ServiceAccountName string
	Container          ContainerSpec
}

func (spec WorkloadSpec) validate() error {
	for label, value := range map[string]string{
		"jobset_name":          spec.Name,
		"replicated_job_name":  spec.ReplicatedJobName,
		"service_account_name": spec.ServiceAccountName,
		"container_name":       spec.Container.Name,
	} {
		if !objectNamePattern.MatchString(value) {
			return invalid(label+"_invalid", label+" is not a valid Kubernetes object name", nil)
		}
	}
	// The restricted-pods policy denies the default ServiceAccount outright: a
	// workload running as `default` inherits whatever that account accumulates.
	if spec.ServiceAccountName == "default" {
		return denied("service_account_default", "workloads must use a dedicated, non-default ServiceAccount")
	}
	if !digestPinnedImage.MatchString(spec.Container.Image) {
		return denied("image_not_digest_pinned", "container image must use an immutable sha256 digest")
	}
	if len(spec.Container.Image) > len(placeholderDigest) &&
		spec.Container.Image[len(spec.Container.Image)-len(placeholderDigest):] == placeholderDigest {
		return denied("image_placeholder_digest", "container image uses the placeholder digest")
	}
	if len(spec.Container.Env) > MaximumEnvironmentEntries {
		return failedPrecondition("environment_bound", "container environment exceeds its bound")
	}
	return nil
}

// ResourceList projects one pod's demand onto a Kubernetes resource list.
//
// pods is deliberately absent: it is a queue-level count, not a container
// request, and emitting it would produce a resource name the kubelet rejects.
// nvidia.com/gpu appears only when positive, because an explicit zero on a
// CPU-only pod would make the pod request an extended resource no CPU node
// advertises.
func ResourceList(demand scheduling.Demand) (corev1.ResourceList, error) {
	if err := demand.Validate(true); err != nil {
		return nil, err
	}
	list := corev1.ResourceList{
		corev1.ResourceCPU:              *resource.NewMilliQuantity(int64(demand[scheduling.ResourceCPU]), resource.DecimalSI),
		corev1.ResourceMemory:           *resource.NewQuantity(int64(demand[scheduling.ResourceMemory]), resource.BinarySI),
		corev1.ResourceEphemeralStorage: *resource.NewQuantity(int64(demand[scheduling.ResourceEphemeralStorage]), resource.BinarySI),
	}
	for _, name := range []scheduling.ResourceName{scheduling.ResourceCPU, scheduling.ResourceMemory, scheduling.ResourceEphemeralStorage} {
		if demand[name] == 0 {
			return nil, invalid("demand_dimension_missing", "every pod must bound cpu, memory, and ephemeral storage", nil)
		}
	}
	if gpu := demand[scheduling.ResourceGPU]; gpu > 0 {
		list[corev1.ResourceName(scheduling.ResourceGPU)] = *resource.NewQuantity(int64(gpu), resource.DecimalSI)
	}
	return list, nil
}

// PodTemplate builds the pod template for one placement.
//
// Requests and limits are the same list. The cluster requires both, and Kueue
// charges the request: letting them diverge would let a workload be admitted
// for one amount and consume another, which is the difference between a quota
// and a suggestion.
func PodTemplate(placement scheduling.Placement, spec WorkloadSpec) (corev1.PodTemplateSpec, error) {
	if err := placement.Validate(); err != nil {
		return corev1.PodTemplateSpec{}, err
	}
	if err := spec.validate(); err != nil {
		return corev1.PodTemplateSpec{}, err
	}
	if placement.PerReplica[scheduling.ResourcePods] != 1 {
		return corev1.PodTemplateSpec{}, invalid("per_replica_pod_count", "a JobSet projection requires exactly one pod per replica", nil)
	}
	resources, err := ResourceList(placement.PerReplica)
	if err != nil {
		return corev1.PodTemplateSpec{}, err
	}
	annotations, err := TopologyAnnotations(placement)
	if err != nil {
		return corev1.PodTemplateSpec{}, err
	}
	labels, err := kueue.WorkloadLabels(placement)
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
			PriorityClassName:            placement.Priority.String(),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   &nonRoot,
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name:    spec.Container.Name,
				Image:   spec.Container.Image,
				Command: append([]string(nil), spec.Container.Command...),
				Args:    append([]string(nil), spec.Container.Args...),
				Env:     append([]corev1.EnvVar(nil), spec.Container.Env...),
				Resources: corev1.ResourceRequirements{
					Requests: resources.DeepCopy(),
					Limits:   resources.DeepCopy(),
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: &escalation,
					Privileged:               &privileged,
					ReadOnlyRootFilesystem:   &readOnlyRoot,
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
		},
	}, nil
}

// Build assembles the JobSet for one placement.
//
// Replicas is the placement's replica count and each Job runs exactly one pod,
// so the object charges the queue precisely what admission reserved. Suspend is
// always true: Kueue admits by unsuspending, and the cluster denies an
// unsuspended batch object in a blocked namespace.
func Build(placement scheduling.Placement, spec WorkloadSpec) (JobSet, error) {
	template, err := PodTemplate(placement, spec)
	if err != nil {
		return JobSet{}, err
	}
	namespace, err := kueue.Namespace(placement)
	if err != nil {
		return JobSet{}, err
	}
	labels, err := kueue.WorkloadLabels(placement)
	if err != nil {
		return JobSet{}, err
	}
	annotations, err := ObjectAnnotations(placement)
	if err != nil {
		return JobSet{}, err
	}
	parallelism := int32(1)
	completions := int32(1)
	backoff := int32(0)
	suspend := true
	object := JobSet{
		TypeMeta: metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: JobSetGVK.Kind},
		ObjectMeta: metav1.ObjectMeta{
			Name:        spec.Name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: JobSetSpec{
			Suspend: &suspend,
			ReplicatedJobs: []ReplicatedJob{{
				Name:     spec.ReplicatedJobName,
				Replicas: int32(placement.Replicas),
				Template: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						Parallelism: &parallelism,
						Completions: &completions,
						// Retries are orchestration's attempt budget, not the
						// Job controller's. A Job that retried on its own would
						// consume reserved capacity for an attempt no
						// execution ticket authorized.
						BackoffLimit: &backoff,
						Template:     template,
					},
				},
			}},
		},
	}
	if err := Verify(object); err != nil {
		return JobSet{}, err
	}
	return object, nil
}

// ToUnstructured renders a JobSet for an applier.
func ToUnstructured(object JobSet) (*unstructured.Unstructured, error) {
	if err := Verify(object); err != nil {
		return nil, err
	}
	fields, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&object)
	if err != nil {
		return nil, invalid("jobset_encode_failed", "jobset object could not be encoded", err)
	}
	return &unstructured.Unstructured{Object: fields}, nil
}

// FromUnstructured decodes a JobSet.
func FromUnstructured(object *unstructured.Unstructured) (JobSet, error) {
	if object == nil {
		return JobSet{}, invalid("object_nil", "jobset object is required", nil)
	}
	var decoded JobSet
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &decoded); err != nil {
		return JobSet{}, invalid("jobset_decode_failed", "jobset object could not be decoded", err)
	}
	return decoded, nil
}

// VerifyJobSet decodes and verifies an unstructured JobSet.
func VerifyJobSet(object *unstructured.Unstructured) error {
	decoded, err := FromUnstructured(object)
	if err != nil {
		return err
	}
	return Verify(decoded)
}

// Verify re-applies the cluster's admission conditions to an object this
// package produced or read back.
//
// It duplicates rules the API server also enforces, on purpose. The API server
// is the authority, but it is reached asynchronously and its denial arrives
// attached to an object rather than to the run that wanted it. Failing here
// turns "a workload silently never started" into "this placement was refused,
// and here is the condition it failed".
func Verify(object JobSet) error {
	if object.APIVersion != GroupVersion.String() || object.Kind != JobSetGVK.Kind {
		return invalid("jobset_kind_invalid", "object is not a jobset.x-k8s.io/v1alpha2 JobSet", nil)
	}
	if !objectNamePattern.MatchString(object.Name) {
		return invalid("jobset_name_invalid", "jobset name is not a valid Kubernetes object name", nil)
	}
	domain, err := kueue.DomainForObjectName(object.Namespace)
	if err != nil {
		return err
	}
	if object.Labels[kueue.QueueNameLabel] != domain.QueueName() ||
		object.Labels[kueue.WorkloadClassLabel] != string(domain.WorkloadClass()) {
		return denied("jobset_triple_invalid", "jobset namespace, queue name, and workload class do not form one capacity triple")
	}
	if object.Spec.Suspend == nil || !*object.Spec.Suspend {
		return denied("jobset_not_suspended", "a jobset must be created suspended")
	}
	if len(object.Spec.ReplicatedJobs) != 1 {
		return invalid("jobset_replicated_job_count", "this projection builds exactly one replicated job", nil)
	}
	job := object.Spec.ReplicatedJobs[0]
	if !objectNamePattern.MatchString(job.Name) {
		return invalid("replicated_job_name_invalid", "replicated job name is not a valid Kubernetes object name", nil)
	}
	if job.Replicas <= 0 || uint32(job.Replicas) > scheduling.MaximumReplicas {
		return invalid("replicated_job_replicas_invalid", "replicated job replica count is outside bounds", nil)
	}
	return verifyPodSpec(job.Template.Spec.Template.Spec)
}

func verifyPodSpec(spec corev1.PodSpec) error {
	if spec.ServiceAccountName == "" || spec.ServiceAccountName == "default" {
		return denied("service_account_invalid", "pods must use a dedicated, non-default ServiceAccount")
	}
	if spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken {
		return denied("automount_token_enabled", "pods must explicitly disable automatic ServiceAccount token mounts")
	}
	if spec.HostNetwork || spec.HostPID || spec.HostIPC {
		return denied("host_namespace_used", "host networking, PID, and IPC are forbidden")
	}
	for _, volume := range spec.Volumes {
		if volume.HostPath != nil {
			return denied("host_path_volume", "hostPath volumes are forbidden")
		}
	}
	if len(spec.EphemeralContainers) > 0 {
		return denied("ephemeral_container_used", "ephemeral containers are forbidden")
	}
	if spec.SecurityContext == nil || spec.SecurityContext.RunAsNonRoot == nil || !*spec.SecurityContext.RunAsNonRoot ||
		spec.SecurityContext.SeccompProfile == nil || spec.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		return denied("pod_security_context_invalid", "pods must require non-root execution and RuntimeDefault seccomp")
	}
	if len(spec.Containers) != 1 {
		return invalid("container_count_invalid", "this projection builds exactly one container", nil)
	}
	// Init containers are held to the same conditions by the cluster policy.
	// This projection emits none, so any present came from somewhere else.
	if len(spec.InitContainers) > 0 {
		return invalid("init_container_unexpected", "this projection builds no init containers", nil)
	}
	return verifyContainer(spec.Containers[0])
}

func verifyContainer(container corev1.Container) error {
	if !digestPinnedImage.MatchString(container.Image) {
		return denied("image_not_digest_pinned", "container image must use an immutable sha256 digest")
	}
	security := container.SecurityContext
	if security == nil || security.AllowPrivilegeEscalation == nil || *security.AllowPrivilegeEscalation ||
		security.ReadOnlyRootFilesystem == nil || !*security.ReadOnlyRootFilesystem ||
		security.Privileged != nil && *security.Privileged {
		return denied("container_security_context_invalid", "containers must be unprivileged and read-only")
	}
	dropped := false
	if security.Capabilities != nil {
		for _, capability := range security.Capabilities.Drop {
			if capability == "ALL" {
				dropped = true
			}
		}
	}
	if !dropped {
		return denied("capabilities_not_dropped", "containers must drop all Linux capabilities")
	}
	for _, list := range []corev1.ResourceList{container.Resources.Requests, container.Resources.Limits} {
		for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourceEphemeralStorage} {
			quantity, present := list[name]
			if !present || quantity.IsZero() {
				return denied("container_resources_unbounded", "containers must bound cpu, memory, and ephemeral storage with requests and limits")
			}
		}
	}
	return nil
}

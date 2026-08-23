// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package jobset projects a sealed scheduling placement onto the JobSet object
// the cluster will accept, and projects an observed JobSet back.
//
// It holds no client library and no controller. The object reader and applier
// are interfaces the composition root injects; everything else here is a pure
// projection whose output is checked against the cluster's own admission
// policies before it is ever sent.
//
// Building the object here rather than in the composition root is deliberate.
// The restricted-pods ValidatingAdmissionPolicy denies on eleven separate
// conditions -- digest-pinned images, non-root, RuntimeDefault seccomp,
// read-only root filesystem, dropped capabilities, bounded CPU/memory/ephemeral
// storage on every container and init container, a dedicated ServiceAccount,
// no host namespaces, no hostPath, no ephemeral containers -- and a projection
// that satisfies all of them is a thing worth writing exactly once.
package jobset

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"go.mindclade.dev/libs/go/faults"
)

const (
	// GroupName and Version pin the JobSet API the cluster's admission policy
	// names. The policy matches jobset.x-k8s.io/v1alpha2 explicitly, so an
	// object built for another version would bypass the rule that keeps batch
	// work suspended in a blocked namespace.
	GroupName = "jobset.x-k8s.io"
	Version   = "v1alpha2"

	operation = "control.scheduling.adapters.jobset"

	// PlacementDigestAnnotation records the sealed placement an object was
	// built from, so a live object can be matched back to the decision that
	// authorized it rather than to a decision that merely looks like it.
	PlacementDigestAnnotation = "mindclade.dev/placement-digest"
	// TopologyDigestAnnotation records the topology contract the placement was
	// resolved against.
	TopologyDigestAnnotation = "mindclade.dev/topology-digest"
	// SnapshotDigestAnnotation records the fleet snapshot the decision was
	// taken against.
	SnapshotDigestAnnotation = "mindclade.dev/fleet-snapshot-digest"
)

// GroupVersion is the pinned JobSet API group/version.
var GroupVersion = schema.GroupVersion{Group: GroupName, Version: Version}

// JobSetGVK is the only kind this adapter builds or reads.
var JobSetGVK = GroupVersion.WithKind("JobSet")

// Reader fetches one cluster object.
type Reader interface {
	Get(ctx context.Context, gvk schema.GroupVersionKind, key types.NamespacedName) (*unstructured.Unstructured, error)
}

// Applier server-side applies one cluster object. The composition root owns
// field management and conflict policy; this package owns the object's content.
type Applier interface {
	Apply(ctx context.Context, object *unstructured.Unstructured) error
}

// Client reads and applies JobSet objects through injected collaborators.
type Client struct {
	Reader  Reader
	Applier Applier
}

// JobSet reads one JobSet object.
func (client Client) JobSet(ctx context.Context, namespace, name string) (*unstructured.Unstructured, error) {
	if ctx == nil {
		return nil, invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if client.Reader == nil {
		return nil, unavailable("reader_unavailable", "jobset object reader is unavailable", nil)
	}
	if namespace == "" || name == "" {
		return nil, invalid("object_key_required", "object namespace and name are required", nil)
	}
	object, err := client.Reader.Get(ctx, JobSetGVK, types.NamespacedName{Namespace: namespace, Name: name})
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, notFound("object_not_found", "jobset object was not found")
	}
	if object.GetKind() != JobSetGVK.Kind || object.GetAPIVersion() != GroupVersion.String() {
		return nil, failedPrecondition("object_kind_mismatch", "reader returned an object of a different kind")
	}
	return object, nil
}

// Apply sends one already-validated JobSet object.
//
// It re-validates the object rather than trusting the caller. Apply is the last
// place a malformed object can be stopped before it reaches an API server whose
// only remaining answer is Deny, and a denial there is an asynchronous failure
// attached to a run rather than a synchronous error attached to a decision.
func (client Client) Apply(ctx context.Context, object *unstructured.Unstructured) error {
	if ctx == nil {
		return invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if client.Applier == nil {
		return unavailable("applier_unavailable", "jobset object applier is unavailable", nil)
	}
	if err := VerifyJobSet(object); err != nil {
		return err
	}
	return client.Applier.Apply(ctx, object)
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

func failedPrecondition(reason, message string) error {
	return faults.New(faults.CodeFailedPrecondition, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func denied(reason, message string) error {
	return faults.New(faults.CodePermissionDenied, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()))
}

func unavailable(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeUnavailable, message,
			faults.WithReason(reason), faults.WithOperation(operation),
			faults.WithRetryPolicy(faults.BackoffRetry(3)))
	}
	return faults.Wrap(cause, faults.CodeUnavailable, message,
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.BackoffRetry(3)))
}

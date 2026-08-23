// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package kueue projects Kueue objects onto scheduling domain values and back.
//
// It is a translation layer and nothing more. It holds no client library, no
// connection, and no reconcile loop: the object reader is an interface the
// composition root injects, and every function here is a pure projection over
// an already-fetched object.
//
// The Kueue objects themselves are GitOps-owned. infra/kubernetes declares the
// Topology, the three ResourceFlavors, and the three ClusterQueue/LocalQueue
// pairs with ArgoCD sync waves, non-zero quota gated behind reviewed activation
// evidence, and stopPolicy Hold. This package therefore READS them and refuses
// to write them: a scheduler that could edit its own quota would be able to
// grant itself capacity the activation review withheld.
package kueue

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"go.mindclade.dev/libs/go/faults"
)

const (
	// GroupName and Version pin the exact API the manifests declare. A newer
	// Kueue version is a reviewed migration, not something this adapter should
	// silently negotiate: the object shapes it parses below are version-specific.
	GroupName = "kueue.x-k8s.io"
	Version   = "v1beta2"

	operation = "control.scheduling.adapters.kueue"

	// QueueNameLabel and WorkloadClassLabel are two thirds of the triple the
	// cluster's ValidatingAdmissionPolicy denies on.
	QueueNameLabel     = "kueue.x-k8s.io/queue-name"
	WorkloadClassLabel = "mindclade.dev/workload-class"
	// PriorityClassLabel selects the Kubernetes PriorityClass Kueue uses to
	// order a workload.
	PriorityClassLabel = "kueue.x-k8s.io/priority-class"

	// RequiredTopologyAnnotation and PreferredTopologyAnnotation are mutually
	// exclusive on a pod template, which is why TopologyConstraint models them
	// as one choice rather than two independent fields.
	RequiredTopologyAnnotation  = "kueue.x-k8s.io/podset-required-topology"
	PreferredTopologyAnnotation = "kueue.x-k8s.io/podset-preferred-topology"

	// StopPolicyHold is the held state every queue ships in.
	StopPolicyHold = "Hold"
	// StopPolicyNone is the only stop policy that admits work.
	StopPolicyNone = "None"
)

// GroupVersion is the pinned Kueue API group/version.
var GroupVersion = schema.GroupVersion{Group: GroupName, Version: Version}

// The object kinds this adapter reads.
var (
	ClusterQueueGVK   = GroupVersion.WithKind("ClusterQueue")
	LocalQueueGVK     = GroupVersion.WithKind("LocalQueue")
	ResourceFlavorGVK = GroupVersion.WithKind("ResourceFlavor")
	TopologyGVK       = GroupVersion.WithKind("Topology")
	WorkloadGVK       = GroupVersion.WithKind("Workload")
)

// Reader fetches one cluster object. The composition root supplies it; this
// package neither builds nor owns a Kubernetes client.
type Reader interface {
	Get(ctx context.Context, gvk schema.GroupVersionKind, key types.NamespacedName) (*unstructured.Unstructured, error)
}

// Client reads Kueue objects through an injected Reader.
type Client struct {
	Reader Reader
}

func (client Client) get(ctx context.Context, gvk schema.GroupVersionKind, key types.NamespacedName) (*unstructured.Unstructured, error) {
	if ctx == nil {
		return nil, invalid("context_nil", "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if client.Reader == nil {
		return nil, unavailable("reader_unavailable", "kueue object reader is unavailable", nil)
	}
	if key.Name == "" {
		return nil, invalid("object_name_required", "object name is required", nil)
	}
	object, err := client.Reader.Get(ctx, gvk, key)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, notFound("object_not_found", "kueue object was not found")
	}
	if object.GetKind() != gvk.Kind || object.GetAPIVersion() != gvk.GroupVersion().String() {
		return nil, failedPrecondition("object_kind_mismatch", "reader returned an object of a different kind")
	}
	return object, nil
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

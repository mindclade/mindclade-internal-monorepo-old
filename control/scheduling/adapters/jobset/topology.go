// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package jobset

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"go.mindclade.dev/control/scheduling"
	"go.mindclade.dev/control/scheduling/adapters/kueue"
)

// TopologyAnnotations returns the pod-template annotations for one placement:
// the Kueue podset topology request, plus the digest of the topology contract
// it was resolved against.
//
// The digest is carried on the object because the Kueue Topology is immutable:
// the only way it changes is replacement, and a live workload whose annotation
// names a topology digest the cluster no longer has is a workload placed
// against boundaries that no longer exist. Recording it makes that detectable
// without keeping a side table.
func TopologyAnnotations(placement scheduling.Placement) (map[string]string, error) {
	annotations, err := kueue.PodSetAnnotations(placement)
	if err != nil {
		return nil, err
	}
	if len(annotations) > 0 {
		annotations[TopologyDigestAnnotation] = placement.TopologyDigest.String()
	}
	return annotations, nil
}

// ObjectAnnotations returns the provenance annotations for the JobSet itself:
// which placement authorized it, which fleet snapshot that placement was
// decided against, and which topology contract resolved its constraint.
//
// These are digests, not identifiers with meaning of their own. A live object
// can be matched back to the exact decision that produced it, and an object
// whose annotations do not match any decision this scheduler made is one it did
// not create, whatever its labels say.
func ObjectAnnotations(placement scheduling.Placement) (map[string]string, error) {
	if err := placement.Validate(); err != nil {
		return nil, err
	}
	return map[string]string{
		PlacementDigestAnnotation: placement.Digest.String(),
		SnapshotDigestAnnotation:  placement.SnapshotDigest.String(),
		TopologyDigestAnnotation:  placement.TopologyDigest.String(),
	}, nil
}

// VerifyPlacementAnnotations checks that a live object still belongs to one
// placement. It is the reconcile-time question "is this the object my decision
// created, or one that drifted", answered without trusting names.
func VerifyPlacementAnnotations(object *unstructured.Unstructured, placement scheduling.Placement) error {
	if object == nil {
		return invalid("object_nil", "jobset object is required", nil)
	}
	expected, err := ObjectAnnotations(placement)
	if err != nil {
		return err
	}
	observed := object.GetAnnotations()
	for key, value := range expected {
		if observed[key] != value {
			return failedPrecondition("placement_annotation_mismatch", "object does not carry the placement provenance it was built with")
		}
	}
	return nil
}

// VerifyTopologyContract checks a placement's sealed topology digest against
// the topology this build was compiled against. A mismatch means the immutable
// Topology object was replaced after the placement was decided, and the
// constraint must be re-resolved rather than re-applied.
func VerifyTopologyContract(placement scheduling.Placement) error {
	if err := placement.Validate(); err != nil {
		return err
	}
	if !placement.TopologyDigest.Equal(placement.Topology.Fingerprint()) {
		return failedPrecondition("topology_contract_changed", "placement topology digest does not match the compiled topology contract")
	}
	return nil
}

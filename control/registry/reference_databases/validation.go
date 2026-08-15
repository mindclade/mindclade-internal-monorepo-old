// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package reference_databases

import (
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"sort"
)

func invalid(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.registry.reference_databases"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.registry.reference_databases"), faults.WithRetryPolicy(faults.NoRetry()))
}
func (r Release) Validate() error {
	if _, err := identifiers.ParseID(r.ReleaseID); err != nil {
		return invalid("reference_release_id_invalid", "reference release id must be canonical", err)
	}
	if r.Name == "" || r.Version == "" || !r.Kind.Valid() || !r.SnapshotDigest.Valid() || r.SourceCutoff.IsZero() || r.Created.IsZero() {
		return invalid("reference_release_fields_required", "reference release identity, kind, snapshot, cutoff, and creation time are required", nil)
	}
	if r.IndexFormat == "" || r.IndexTool == "" || r.IndexToolVersion == "" || !r.SourceProvenanceDigest.Valid() || !r.LicensePolicyDigest.Valid() {
		return invalid("reference_release_provenance_required", "reference release index and provenance metadata are required", nil)
	}
	if len(r.Shards) == 0 {
		return invalid("reference_release_shards_required", "reference release requires immutable shards", nil)
	}
	for _, s := range r.Shards {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	if !sort.StringsAreSorted(r.CompatibleSearchTools) {
		return invalid("reference_release_tools_not_canonical", "compatible search tools must be sorted", nil)
	}
	expected := identifiers.SHA256([]byte(r.canonical(false)))
	if !r.SnapshotDigest.Equal(expected) {
		return invalid("reference_snapshot_digest_mismatch", "reference database snapshot digest does not match release content", nil)
	}
	return nil
}

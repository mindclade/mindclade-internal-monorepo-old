// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package artifacts

import (
	"errors"

	"go.mindclade.dev/libs/go/faults"
)

// Reason codes every Catalog implementation must use for the four domain
// rejections. A caller decides what to do from the reason, so an in-memory
// catalog and a durable one that disagreed on the string would make the seam
// untestable: a test written against one implementation would silently pass
// against the other while asserting nothing.
const (
	ReasonIdentityConflict        = "artifact_identity_conflict"
	ReasonLocationUnknownIdentity = "artifact_location_unknown_identity"
	ReasonNotFound                = "artifact_not_found"
	ReasonLocationBudget          = "artifact_location_budget_exhausted"
)

// Sentinels behind those reasons. They exist so an adapter in another package
// can produce the identical rejection -- same reason, same code, same
// errors.Is target -- without re-deriving the domain's wording, and so a test
// can assert one contract against every implementation.
var (
	// ErrIdentityConflict reports a digest re-registered with different
	// immutable metadata. The binding is permanent, so this is never a
	// transient condition and never retryable.
	ErrIdentityConflict = errors.New("artifacts: digest is already registered with different immutable metadata")

	// ErrLocationUnknownIdentity reports a location written for a digest whose
	// identity is absent, or present with different immutable metadata.
	ErrLocationUnknownIdentity = errors.New("artifacts: artifact identity must be registered before a location")

	// ErrNotFound reports a digest that is not registered.
	ErrNotFound = errors.New("artifacts: artifact is not registered")

	// ErrLocationBudget reports an artifact already holding
	// MaximumLocationsPerArtifact distinct locations. The catalog has no
	// eviction, so an unbounded location set is a durable leak, not a
	// transient overload.
	ErrLocationBudget = errors.New("artifacts: artifact location budget is exhausted")
)

func invalid(reason, message string, cause error) error {
	if cause == nil {
		return faults.New(faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.artifacts"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("control.artifacts"), faults.WithRetryPolicy(faults.NoRetry()))
}
func notFound(reason, message string) error {
	return faults.New(faults.CodeNotFound, message, faults.WithReason(reason), faults.WithOperation("control.artifacts"), faults.WithRetryPolicy(faults.NoRetry()))
}

func identityConflict() error {
	return invalid(ReasonIdentityConflict, "digest is already registered with different immutable metadata", ErrIdentityConflict)
}

func locationUnknownIdentity() error {
	return invalid(ReasonLocationUnknownIdentity, "artifact identity must be registered before a location", ErrLocationUnknownIdentity)
}

func notRegistered() error {
	return faults.Wrap(ErrNotFound, faults.CodeNotFound, "artifact is not registered",
		faults.WithReason(ReasonNotFound), faults.WithOperation("control.artifacts"), faults.WithRetryPolicy(faults.NoRetry()))
}

func locationBudgetExhausted() error {
	return faults.Wrap(ErrLocationBudget, faults.CodeResourceExhausted, "artifact location budget is exhausted",
		faults.WithReason(ReasonLocationBudget), faults.WithOperation("control.artifacts"), faults.WithRetryPolicy(faults.NoRetry()))
}

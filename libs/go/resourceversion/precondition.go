// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package resourceversion

import "go.mindclade.dev/libs/go/faults"

// Precondition describes the durable existence/version condition required by a
// mutation. At most one mode may be selected.
type Precondition struct {
	MustExist    bool
	MustNotExist bool
	Match        Version
}

func MatchVersion(version Version) Precondition { return Precondition{Match: version} }
func RequireExistence() Precondition            { return Precondition{MustExist: true} }
func RequireAbsence() Precondition              { return Precondition{MustNotExist: true} }

func (precondition Precondition) Validate() error {
	modes := 0
	if precondition.MustExist {
		modes++
	}
	if precondition.MustNotExist {
		modes++
	}
	if !precondition.Match.IsZero() {
		modes++
		if err := precondition.Match.Validate(); err != nil {
			return err
		}
	}
	if modes > 1 {
		return faults.Wrap(ErrInvalidPrecondition, faults.CodeInvalidArgument, "resource precondition selects multiple modes", faults.WithReason("conflicting_resource_precondition"), faults.WithOperation("resourceversion.Precondition.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

// Check evaluates the precondition against the current resource state.
func (precondition Precondition) Check(exists bool, current Version) error {
	if err := precondition.Validate(); err != nil {
		return err
	}
	failed := precondition.MustExist && !exists || precondition.MustNotExist && exists || !precondition.Match.IsZero() && (!exists || current != precondition.Match)
	if !failed {
		return nil
	}
	return faults.Wrap(ErrPreconditionFailed, faults.CodeConflict, "resource precondition failed", faults.WithReason("resource_precondition_failed"), faults.WithOperation("resourceversion.Precondition.Check"), faults.WithField("exists", exists), faults.WithField("current_version", current.String()), faults.WithField("expected_version", precondition.Match.String()), faults.WithRetryPolicy(faults.NoRetry()))
}

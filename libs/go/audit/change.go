// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package audit

import (
	"slices"
	"sort"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

const MaximumChangedFieldCount = 128

// Change records content digests and bounded field names without embedding
// potentially sensitive before/after values.
type Change struct {
	before identifiers.Digest
	after  identifiers.Digest
	fields []string
}

func NewChange(before, after identifiers.Digest, fields ...string) (Change, error) {
	change := Change{before: before, after: after, fields: slices.Clone(fields)}
	sort.Strings(change.fields)
	change.fields = slices.Compact(change.fields)
	if err := change.Validate(); err != nil {
		return Change{}, err
	}
	return change, nil
}

func (change Change) BeforeDigest() identifiers.Digest { return change.before }
func (change Change) AfterDigest() identifiers.Digest  { return change.after }
func (change Change) Fields() []string                 { return slices.Clone(change.fields) }
func (change Change) IsZero() bool {
	return change.before.IsZero() && change.after.IsZero() && len(change.fields) == 0
}

func (change Change) Validate() error {
	if !change.before.IsZero() && !change.before.Valid() || !change.after.IsZero() && !change.after.Valid() {
		return invalidChange("invalid_change_digest")
	}
	if len(change.fields) > MaximumChangedFieldCount {
		return invalidChange("too_many_changed_fields")
	}
	for _, field := range change.fields {
		if !validCanonicalName(field, MaximumFieldKeyLength, false) {
			return invalidChange("invalid_changed_field")
		}
	}
	return nil
}

func invalidChange(reason string) error {
	return faults.Wrap(ErrInvalidChange, faults.CodeInvalidArgument, "invalid audit change", faults.WithReason(reason), faults.WithOperation("audit.Change.Validate"))
}

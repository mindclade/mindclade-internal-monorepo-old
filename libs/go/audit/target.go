// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package audit

import (
	"encoding/json"
	"errors"
	"strings"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

// Target identifies the resource affected by an audited operation. ID may be
// zero for collection-level or configuration-level operations.
type Target struct {
	targetType string
	id         identifiers.ID
	parentID   identifiers.ID
	name       string
}

type TargetOption func(*Target) error

func WithTargetID(identifier identifiers.ID) TargetOption {
	return func(target *Target) error { target.id = identifier; return nil }
}
func WithParentTargetID(identifier identifiers.ID) TargetOption {
	return func(target *Target) error { target.parentID = identifier; return nil }
}
func WithTargetName(name string) TargetOption {
	return func(target *Target) error { target.name = name; return nil }
}

func NewTarget(targetType string, options ...TargetOption) (Target, error) {
	target := Target{targetType: strings.TrimSpace(targetType)}
	for _, option := range options {
		if option != nil {
			if err := option(&target); err != nil {
				return Target{}, err
			}
		}
	}
	target.name = strings.TrimSpace(target.name)
	if err := target.Validate(); err != nil {
		return Target{}, err
	}
	return target, nil
}

func (target Target) Type() string             { return target.targetType }
func (target Target) ID() identifiers.ID       { return target.id }
func (target Target) ParentID() identifiers.ID { return target.parentID }
func (target Target) Name() string             { return target.name }

func (target Target) Validate() error {
	if !validCanonicalName(target.targetType, MaximumTargetTypeLength, false) {
		return invalidTarget("invalid_target_type")
	}
	for _, identifier := range []identifiers.ID{target.id, target.parentID} {
		if !identifier.IsZero() {
			if err := identifier.Validate(); err != nil {
				return invalidTarget("invalid_target_identifier", err)
			}
		}
	}
	if !target.id.IsZero() && target.id == target.parentID {
		return invalidTarget("target_parent_cycle")
	}
	if !validDisplayText(target.name, MaximumTargetNameLength) {
		return invalidTarget("invalid_target_name")
	}
	return nil
}

func invalidTarget(reason string, causes ...error) error {
	cause := error(ErrInvalidTarget)
	if len(causes) > 0 && causes[0] != nil {
		cause = errors.Join(ErrInvalidTarget, causes[0])
	}
	return faults.Wrap(cause, faults.CodeInvalidArgument, "invalid audit target", faults.WithReason(reason), faults.WithOperation("audit.Target.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
}

type targetJSON struct {
	Type     string `json:"type"`
	ID       string `json:"id,omitempty"`
	ParentID string `json:"parent_id,omitempty"`
	Name     string `json:"name,omitempty"`
}

func (target Target) MarshalJSON() ([]byte, error) {
	if err := target.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(targetJSON{Type: target.targetType, ID: target.id.String(), ParentID: target.parentID.String(), Name: target.name})
}

func (target *Target) UnmarshalJSON(value []byte) error {
	if target == nil {
		return invalidTarget("nil_audit_target")
	}
	var wire targetJSON
	if err := json.Unmarshal(value, &wire); err != nil {
		return invalidTarget("malformed_audit_target", err)
	}
	options := []TargetOption{WithTargetName(wire.Name)}
	if wire.ID != "" {
		identifier, err := identifiers.ParseID(wire.ID)
		if err != nil {
			return invalidTarget("invalid_target_identifier", err)
		}
		options = append(options, WithTargetID(identifier))
	}
	if wire.ParentID != "" {
		identifier, err := identifiers.ParseID(wire.ParentID)
		if err != nil {
			return invalidTarget("invalid_parent_target_identifier", err)
		}
		options = append(options, WithParentTargetID(identifier))
	}
	parsed, err := NewTarget(wire.Type, options...)
	if err != nil {
		return err
	}
	*target = parsed
	return nil
}

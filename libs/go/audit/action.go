// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package audit

import (
	"strings"

	"go.mindclade.dev/libs/go/faults"
)

// Action is a stable lower-case operation name such as "runs.cancel" or
// "models.release.promote".
type Action string

func ParseAction(value string) (Action, error) {
	action := Action(strings.TrimSpace(value))
	if !action.Valid() {
		return "", faults.Wrap(
			ErrInvalidAction,
			faults.CodeInvalidArgument,
			"invalid audit action",
			faults.WithReason("invalid_audit_action"),
			faults.WithOperation("audit.ParseAction"),
			faults.WithField("action", value),
		)
	}
	return action, nil
}

func MustParseAction(value string) Action {
	action, err := ParseAction(value)
	if err != nil {
		panic(err)
	}
	return action
}

func (action Action) String() string { return string(action) }
func (action Action) Valid() bool {
	return validCanonicalName(string(action), MaximumActionLength, true)
}
func (action Action) MarshalText() ([]byte, error) {
	if !action.Valid() {
		return nil, ErrInvalidAction
	}
	return []byte(action), nil
}
func (action *Action) UnmarshalText(value []byte) error {
	if action == nil {
		return faults.Wrap(ErrInvalidAction, faults.CodeInvalidArgument, "invalid audit action", faults.WithReason("invalid_audit_action"), faults.WithOperation("audit.Action.UnmarshalText"))
	}
	parsed, err := ParseAction(string(value))
	if err != nil {
		return err
	}
	*action = parsed
	return nil
}

// Outcome is the terminal result represented by one audit event.
type Outcome string

const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
	OutcomeDenied    Outcome = "denied"
)

func (outcome Outcome) String() string { return string(outcome) }
func (outcome Outcome) Valid() bool {
	switch outcome {
	case OutcomeSucceeded, OutcomeFailed, OutcomeDenied:
		return true
	default:
		return false
	}
}

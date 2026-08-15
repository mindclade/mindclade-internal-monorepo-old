// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package identifiers

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	// ErrInvalid reports that a parsed or constructed identifier value is not
	// valid. More specific sentinels may also match the same error.
	ErrInvalid = errors.New("identifiers: invalid value")

	ErrInvalidKind   = errors.New("identifiers: invalid kind")
	ErrInvalidID     = errors.New("identifiers: invalid id")
	ErrInvalidUUID   = errors.New("identifiers: invalid uuid")
	ErrInvalidDigest = errors.New("identifiers: invalid digest")
	ErrEntropy       = errors.New("identifiers: entropy source failed")
	ErrTimeRange     = errors.New("identifiers: timestamp outside uuidv7 range")
)

// ValidationError describes a rejected identifier value. It supports
// errors.Is for ErrInvalid and for each error supplied as a cause.
type ValidationError struct {
	Type   string
	Value  string
	Reason string
	causes []error
}

var _ error = (*ValidationError)(nil)

func (validation *ValidationError) Error() string {
	if validation == nil {
		return "<nil>"
	}

	valueType := strings.TrimSpace(validation.Type)
	if valueType == "" {
		valueType = "value"
	}

	message := "identifiers: invalid " + valueType
	if validation.Value != "" {
		message += " " + strconv.Quote(limitDiagnosticValue(validation.Value))
	}
	if reason := strings.TrimSpace(validation.Reason); reason != "" {
		message += ": " + reason
	}
	return message
}

// Unwrap returns ErrInvalid and any more specific causes.
func (validation *ValidationError) Unwrap() []error {
	if validation == nil {
		return nil
	}

	causes := make([]error, 0, len(validation.causes)+1)
	causes = append(causes, ErrInvalid)
	for _, cause := range validation.causes {
		if cause != nil {
			causes = append(causes, cause)
		}
	}
	return causes
}

func invalidValue(valueType string, value string, reason string, causes ...error) error {
	return &ValidationError{
		Type:   valueType,
		Value:  value,
		Reason: reason,
		causes: append([]error(nil), causes...),
	}
}

func limitDiagnosticValue(value string) string {
	const maximum = 160
	if len(value) <= maximum {
		return value
	}
	return fmt.Sprintf("%s…(%d bytes)", value[:maximum], len(value))
}

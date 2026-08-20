// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package audit

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"go.mindclade.dev/libs/go/faults"
)

const (
	MaximumFieldCount       = 64
	MaximumFieldKeyLength   = 64
	MaximumFieldValueLength = 1024
)

// Fields is bounded, non-sensitive audit metadata.
type Fields map[string]string

func (fields Fields) Clone() Fields {
	if len(fields) == 0 {
		return nil
	}
	output := make(Fields, len(fields))
	for key, value := range fields {
		output[key] = value
	}
	return output
}

func (fields Fields) Validate() error {
	if len(fields) > MaximumFieldCount {
		return invalidFields("too_many_audit_fields")
	}
	for key, value := range fields {
		if !validCanonicalName(key, MaximumFieldKeyLength, false) || len(value) > MaximumFieldValueLength || !utf8.ValidString(value) {
			return invalidFields("invalid_audit_field")
		}
		canonical := strings.NewReplacer("-", "_", ".", "_", ":", "_", "/", "_").Replace(strings.ToLower(key))
		for _, sensitive := range []string{"secret", "password", "token", "credential", "private_key", "api_key", "authorization", "cookie"} {
			if strings.Contains(canonical, sensitive) {
				return invalidFields("sensitive_audit_field")
			}
		}
		for _, character := range value {
			if unicode.IsControl(character) {
				return invalidFields("invalid_audit_field_value")
			}
		}
	}
	return nil
}

func invalidFields(reason string) error {
	return faults.Wrap(ErrInvalidFields, faults.CodeInvalidArgument, "invalid audit fields", faults.WithReason(reason), faults.WithOperation("audit.Fields.Validate"))
}

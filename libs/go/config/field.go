// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package config

import (
	"strings"

	"go.mindclade.dev/libs/go/faults"
)

const (
	MaximumKeyBytes   = 128
	MaximumValueBytes = 1 << 20
)

type Validator func(string) error

type Field struct {
	Key        string
	Required   bool
	Secret     bool
	Reloadable bool
	Default    *string
	Validate   Validator
}

func String(value string) *string { captured := value; return &captured }
func (field Field) normalized() Field {
	field.Key = strings.TrimSpace(field.Key)
	if field.Default != nil {
		captured := *field.Default
		field.Default = &captured
	}
	return field
}
func (field Field) ValidateField() error {
	field = field.normalized()
	if !canonicalKey(field.Key) {
		return invalid(ErrInvalidField, "invalid_config_field", "config.Field.Validate", field.Key, "")
	}
	if field.Default != nil && len(*field.Default) > MaximumValueBytes {
		return invalid(ErrInvalidField, "config_default_too_large", "config.Field.Validate", field.Key, "")
	}
	if field.Required && field.Default != nil && *field.Default == "" {
		return invalid(ErrInvalidField, "empty_required_default", "config.Field.Validate", field.Key, "")
	}
	if field.Default != nil && field.Validate != nil {
		if err := field.Validate(*field.Default); err != nil {
			return faults.Wrap(err, faults.CodeInvalidArgument, "invalid configuration default", faults.WithReason("invalid_config_default"), faults.WithOperation("config.Field.Validate"), faults.WithField("key", field.Key), faults.WithRetryPolicy(faults.NoRetry()))
		}
	}
	return nil
}
func canonicalKey(value string) bool {
	if value == "" || len(value) > MaximumKeyBytes || strings.TrimSpace(value) != value {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && (character == '_' || character == '.' || character == '-') {
			continue
		}
		return false
	}
	return true
}

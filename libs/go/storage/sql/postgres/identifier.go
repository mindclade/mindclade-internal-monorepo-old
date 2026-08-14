// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package postgres

import (
	"strings"

	"mindclade.internal/libs/go/faults"
)

const (
	MaximumIdentifierPartBytes      = 63
	MaximumQualifiedIdentifierBytes = 127
)

// ValidQualifiedIdentifier reports whether value is a lower-case unquoted
// PostgreSQL identifier or a schema-qualified pair. Dynamic table names in
// shared adapters must pass this check before string interpolation.
func ValidQualifiedIdentifier(value string) bool {
	if value == "" || len(value) > MaximumQualifiedIdentifierBytes || value != strings.TrimSpace(value) || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return false
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !validIdentifierPart(part) {
			return false
		}
	}
	return true
}

func validIdentifierPart(value string) bool {
	if value == "" || len(value) > MaximumIdentifierPartBytes || !((value[0] >= 'a' && value[0] <= 'z') || value[0] == '_') {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_') {
			return false
		}
	}
	return true
}

// QualifiedIdentifier validates value and returns its normalized form.
func QualifiedIdentifier(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !ValidQualifiedIdentifier(value) {
		return "", faults.New(faults.CodeInvalidArgument, "invalid PostgreSQL identifier", faults.WithReason("invalid_postgres_identifier"), faults.WithOperation("storage.sql.postgres.QualifiedIdentifier"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return value, nil
}

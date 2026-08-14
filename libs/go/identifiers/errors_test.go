// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package identifiers

import (
	"errors"
	"strings"
	"testing"
)

func TestValidationErrorMatchesGeneralAndSpecificSentinels(t *testing.T) {
	t.Parallel()

	_, err := ParseKind("INVALID")
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, ErrInvalidKind) {
		t.Fatalf("error = %v", err)
	}

	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("errors.As() failed for %T", err)
	}
}

func TestValidationErrorLimitsDiagnosticValue(t *testing.T) {
	t.Parallel()

	value := strings.Repeat("x", 1_000)
	err := invalidValue("id", value, "too long", ErrInvalidID)
	if len(err.Error()) >= len(value) {
		t.Fatalf("error did not limit diagnostic value: %d", len(err.Error()))
	}
}

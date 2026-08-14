// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package identifiers

import (
	"errors"
	"strings"
	"testing"
)

func TestParseKind(t *testing.T) {
	t.Parallel()

	valid := []string{"run", "org", "model2", "ab", strings.Repeat("a", MaximumKindLength)}
	for _, value := range valid {
		value := value
		t.Run("valid_"+value, func(t *testing.T) {
			t.Parallel()
			kind, err := ParseKind(value)
			if err != nil {
				t.Fatalf("ParseKind() error = %v", err)
			}
			if kind.String() != value || !kind.Valid() {
				t.Fatalf("kind = %q, valid=%v", kind, kind.Valid())
			}
		})
	}

	invalid := []string{"", "a", "1run", "Run", "run_id", "run-id", " run", strings.Repeat("a", MaximumKindLength+1)}
	for _, value := range invalid {
		value := value
		t.Run("invalid_"+value, func(t *testing.T) {
			t.Parallel()
			_, err := ParseKind(value)
			if !errors.Is(err, ErrInvalid) || !errors.Is(err, ErrInvalidKind) {
				t.Fatalf("ParseKind() error = %v", err)
			}
		})
	}
}

func TestMustParseKindPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("MustParseKind() did not panic")
		}
	}()
	_ = MustParseKind("INVALID")
}

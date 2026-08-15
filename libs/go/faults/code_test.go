// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package faults

import "testing"

func TestCodeRoundTrip(t *testing.T) {
	t.Parallel()

	codes := []Code{
		CodeUnknown,
		CodeCanceled,
		CodeInvalidArgument,
		CodeDeadlineExceeded,
		CodeNotFound,
		CodeAlreadyExists,
		CodePermissionDenied,
		CodeUnauthenticated,
		CodeResourceExhausted,
		CodeFailedPrecondition,
		CodeConflict,
		CodeAborted,
		CodeOutOfRange,
		CodeNotImplemented,
		CodeInternal,
		CodeUnavailable,
		CodeDataLoss,
	}

	for _, code := range codes {
		code := code
		t.Run(code.String(), func(t *testing.T) {
			t.Parallel()

			if !code.Valid() {
				t.Fatalf("%q should be valid", code)
			}

			parsed, err := ParseCode("  " + code.String() + "  ")
			if err != nil {
				t.Fatalf("ParseCode() error = %v", err)
			}
			if parsed != code {
				t.Fatalf("ParseCode() = %q, want %q", parsed, code)
			}
		})
	}
}

func TestParseCodeNormalizesCase(t *testing.T) {
	t.Parallel()

	got, err := ParseCode("NOT_FOUND")
	if err != nil {
		t.Fatalf("ParseCode() error = %v", err)
	}
	if got != CodeNotFound {
		t.Fatalf("ParseCode() = %q, want %q", got, CodeNotFound)
	}
}

func TestParseCodeRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	got, err := ParseCode("made_up")
	if err == nil {
		t.Fatal("ParseCode() error = nil, want non-nil")
	}
	if got != CodeUnknown {
		t.Fatalf("ParseCode() = %q, want %q", got, CodeUnknown)
	}
}

func TestNormalizeCode(t *testing.T) {
	t.Parallel()

	if got := NormalizeCode(CodeNotFound); got != CodeNotFound {
		t.Fatalf("NormalizeCode(valid) = %q", got)
	}
	if got := NormalizeCode(Code("made_up")); got != CodeUnknown {
		t.Fatalf("NormalizeCode(invalid) = %q, want %q", got, CodeUnknown)
	}
}

func TestDefaultMessagesAreNonEmpty(t *testing.T) {
	t.Parallel()

	for code := range validCodes {
		if got := defaultMessage(code); got == "" {
			t.Fatalf("defaultMessage(%q) is empty", code)
		}
	}
}

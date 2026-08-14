// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package coordination

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"mindclade.internal/libs/go/faults"
)

const (
	MaximumFailureReasonLength  = 128
	MaximumFailureMessageLength = 1024
)

// Failure is a bounded, durable summary of an execution failure. It is safe to
// persist and expose through administrative APIs; private error text, stacks,
// and arbitrary fields deliberately remain outside this structure.
type Failure struct {
	Code        faults.Code        `json:"code"`
	Reason      string             `json:"reason"`
	Message     string             `json:"message"`
	RetryPolicy faults.RetryPolicy `json:"retry_policy,omitempty"`
	OccurredAt  time.Time          `json:"occurred_at"`
}

func FailureFromError(err error, occurredAt time.Time) Failure {
	if err == nil {
		return Failure{}
	}
	code := faults.CodeOf(err)
	if code == faults.CodeUnknown {
		code = faults.CodeInternal
	}
	reason := canonicalReason(faults.ReasonOf(err))
	if reason == "" {
		reason = "operation_failed"
	}
	message := strings.TrimSpace(faults.PublicMessageOf(err))
	if message == "" {
		message = "operation failed"
	}
	message = truncateRunes(message, MaximumFailureMessageLength)
	return Failure{
		Code:        code,
		Reason:      reason,
		Message:     message,
		RetryPolicy: faults.RetryPolicyOf(err).Normalized(),
		OccurredAt:  occurredAt.Round(0).UTC(),
	}
}

func (failure Failure) IsZero() bool {
	return failure.Code == "" && failure.Reason == "" && failure.Message == "" &&
		!failure.RetryPolicy.Specified() && failure.OccurredAt.IsZero()
}

func (failure Failure) Validate() error {
	if !failure.Code.Valid() || failure.Code == faults.CodeUnknown ||
		canonicalReason(failure.Reason) != failure.Reason || failure.Reason == "" ||
		!validPublicText(failure.Message, MaximumFailureMessageLength) ||
		!failure.RetryPolicy.Valid() || failure.OccurredAt.IsZero() {
		return faults.Wrap(ErrInvalidFailure, faults.CodeInvalidArgument, "invalid coordination failure",
			faults.WithReason("invalid_coordination_failure"),
			faults.WithOperation("coordination.Failure.Validate"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return nil
}

func canonicalReason(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > MaximumFailureReasonLength {
		return ""
	}
	previousSeparator := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		letter := character >= 'a' && character <= 'z'
		digit := character >= '0' && character <= '9'
		separator := character == '_' || character == '.' || character == '-'
		if !letter && !digit && !separator || index == 0 && separator || index == len(value)-1 && separator || separator && previousSeparator {
			return ""
		}
		previousSeparator = separator
	}
	return value
}

func validPublicText(value string, maximum int) bool {
	if value == "" || len(value) > maximum || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func truncateRunes(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	for len(value) > maximum {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}

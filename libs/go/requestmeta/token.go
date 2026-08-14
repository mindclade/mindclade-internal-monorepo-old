// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package requestmeta

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

const MaximumPropagationIDLength = 128

type propagationID struct {
	value string
}

func parsePropagationID(value string, sentinel error, reason, label string) (propagationID, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || len(normalized) > MaximumPropagationIDLength {
		return propagationID{}, invalidArgument(
			sentinel,
			"invalid "+label,
			reason,
			map[string]any{"value_length": len(normalized)},
		)
	}
	for index := 0; index < len(normalized); index++ {
		character := normalized[index]
		if character < 0x21 || character > 0x7e {
			return propagationID{}, invalidArgument(
				sentinel,
				"invalid "+label,
				reason,
				map[string]any{"value_length": len(normalized)},
			)
		}
	}
	return propagationID{value: normalized}, nil
}

func (identifier propagationID) String() string { return identifier.value }
func (identifier propagationID) IsZero() bool   { return identifier.value == "" }

// CorrelationID groups activity that belongs to one end-to-end logical flow.
// It may originate outside Mindclade, so it is a bounded visible-ASCII token
// rather than a required Mindclade resource ID.
type CorrelationID struct{ propagationID }

func ParseCorrelationID(value string) (CorrelationID, error) {
	parsed, err := parsePropagationID(value, ErrInvalidCorrelation, "invalid_correlation_id", "correlation identifier")
	if err != nil {
		return CorrelationID{}, err
	}
	return CorrelationID{propagationID: parsed}, nil
}

func CorrelationIDFromRequestID(requestID RequestID) (CorrelationID, error) {
	if err := requestID.Validate(); err != nil {
		return CorrelationID{}, err
	}
	return ParseCorrelationID(requestID.String())
}

func (identifier CorrelationID) Valid() bool {
	if identifier.IsZero() {
		return false
	}
	_, err := ParseCorrelationID(identifier.String())
	return err == nil
}

func (identifier CorrelationID) MarshalText() ([]byte, error) {
	if identifier.IsZero() {
		return []byte{}, nil
	}
	if !identifier.Valid() {
		return nil, invalidArgument(ErrInvalidCorrelation, "invalid correlation identifier", "invalid_correlation_id", nil)
	}
	return []byte(identifier.String()), nil
}

func (identifier *CorrelationID) UnmarshalText(value []byte) error {
	if identifier == nil {
		return invalidArgument(ErrInvalidCorrelation, "invalid correlation identifier", "invalid_correlation_id", nil)
	}
	if len(value) == 0 {
		*identifier = CorrelationID{}
		return nil
	}
	parsed, err := ParseCorrelationID(string(value))
	if err != nil {
		return err
	}
	*identifier = parsed
	return nil
}

func (identifier CorrelationID) MarshalJSON() ([]byte, error) {
	if identifier.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(identifier.String())
}

func (identifier *CorrelationID) UnmarshalJSON(value []byte) error {
	if identifier == nil {
		return invalidArgument(ErrInvalidCorrelation, "invalid correlation identifier", "invalid_correlation_id", nil)
	}
	if bytes.Equal(value, []byte("null")) {
		*identifier = CorrelationID{}
		return nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return invalidArgument(errors.Join(ErrInvalidCorrelation, err), "invalid correlation identifier", "invalid_correlation_id", nil)
	}
	return identifier.UnmarshalText([]byte(text))
}

// CausationID identifies the request, message, event, or command that directly
// caused the current operation.
type CausationID struct{ propagationID }

func ParseCausationID(value string) (CausationID, error) {
	parsed, err := parsePropagationID(value, ErrInvalidCausation, "invalid_causation_id", "causation identifier")
	if err != nil {
		return CausationID{}, err
	}
	return CausationID{propagationID: parsed}, nil
}

func CausationIDFromRequestID(requestID RequestID) (CausationID, error) {
	if err := requestID.Validate(); err != nil {
		return CausationID{}, err
	}
	return ParseCausationID(requestID.String())
}

func (identifier CausationID) Valid() bool {
	if identifier.IsZero() {
		return false
	}
	_, err := ParseCausationID(identifier.String())
	return err == nil
}

func (identifier CausationID) MarshalText() ([]byte, error) {
	if identifier.IsZero() {
		return []byte{}, nil
	}
	if !identifier.Valid() {
		return nil, invalidArgument(ErrInvalidCausation, "invalid causation identifier", "invalid_causation_id", nil)
	}
	return []byte(identifier.String()), nil
}

func (identifier *CausationID) UnmarshalText(value []byte) error {
	if identifier == nil {
		return invalidArgument(ErrInvalidCausation, "invalid causation identifier", "invalid_causation_id", nil)
	}
	if len(value) == 0 {
		*identifier = CausationID{}
		return nil
	}
	parsed, err := ParseCausationID(string(value))
	if err != nil {
		return err
	}
	*identifier = parsed
	return nil
}

func (identifier CausationID) MarshalJSON() ([]byte, error) {
	if identifier.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(identifier.String())
}

func (identifier *CausationID) UnmarshalJSON(value []byte) error {
	if identifier == nil {
		return invalidArgument(ErrInvalidCausation, "invalid causation identifier", "invalid_causation_id", nil)
	}
	if bytes.Equal(value, []byte("null")) {
		*identifier = CausationID{}
		return nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return invalidArgument(errors.Join(ErrInvalidCausation, err), "invalid causation identifier", "invalid_causation_id", nil)
	}
	return identifier.UnmarshalText([]byte(text))
}

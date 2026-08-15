// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package requestmeta

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

const RequestIDKind identifiers.Kind = "request"

// RequestID is a canonical Mindclade request identifier backed by a UUIDv7
// resource ID with kind "request". Its zero value represents absence.
type RequestID struct {
	value identifiers.ID
}

// NewRequestID creates a new process-monotonic request identifier.
func NewRequestID() (RequestID, error) {
	identifier, err := identifiers.NewID(RequestIDKind)
	if err != nil {
		return RequestID{}, faults.Wrap(
			err,
			faults.CodeInternal,
			"unable to generate request identifier",
			faults.WithReason("request_id_generation_failed"),
			faults.WithOperation("requestmeta.NewRequestID"),
			faults.WithRetryPolicy(faults.BackoffRetry(3)),
		)
	}
	return RequestID{value: identifier}, nil
}

// NewRequestIDAt creates a request identifier using timestamp as the requested
// UUIDv7 time. The underlying process-wide generator preserves monotonic order,
// so the embedded time may be later when timestamp precedes an already-issued
// identifier. It is intended primarily for fixtures and migrations.
func NewRequestIDAt(timestamp time.Time) (RequestID, error) {
	identifier, err := identifiers.NewIDAt(RequestIDKind, timestamp)
	if err != nil {
		return RequestID{}, faults.Wrap(
			err,
			faults.CodeInvalidArgument,
			"unable to generate request identifier",
			faults.WithReason("invalid_request_id_timestamp"),
			faults.WithOperation("requestmeta.NewRequestIDAt"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return RequestID{value: identifier}, nil
}

// RequestIDFromID validates identifier and converts it to a RequestID.
func RequestIDFromID(identifier identifiers.ID) (RequestID, error) {
	if err := identifier.Validate(); err != nil {
		return RequestID{}, invalidRequestID(errors.Join(ErrInvalidRequestID, err), identifier.String())
	}
	if identifier.Kind() != RequestIDKind {
		return RequestID{}, invalidRequestID(
			errors.Join(ErrInvalidRequestID, fmt.Errorf("expected kind %q, received %q", RequestIDKind, identifier.Kind())),
			identifier.String(),
		)
	}
	return RequestID{value: identifier}, nil
}

// ParseRequestID parses a canonical request resource ID.
func ParseRequestID(value string) (RequestID, error) {
	identifier, err := identifiers.ParseIDKind(value, RequestIDKind)
	if err != nil {
		return RequestID{}, invalidRequestID(errors.Join(ErrInvalidRequestID, err), value)
	}
	return RequestID{value: identifier}, nil
}

// MustParseRequestID is ParseRequestID for constants and fixtures.
func MustParseRequestID(value string) RequestID {
	requestID, err := ParseRequestID(value)
	if err != nil {
		panic(err)
	}
	return requestID
}

func invalidRequestID(cause error, value string) error {
	fields := faults.Fields{}
	if value != "" {
		fields[faults.FieldRequestID] = value
	}
	return invalidArgument(cause, "invalid request identifier", "invalid_request_id", fields)
}

// String returns the canonical ID or an empty string for the zero value.
func (requestID RequestID) String() string {
	return requestID.value.String()
}

// ID returns the underlying generic resource identifier.
func (requestID RequestID) ID() identifiers.ID {
	return requestID.value
}

// IsZero reports whether requestID is absent.
func (requestID RequestID) IsZero() bool {
	return requestID.value.IsZero()
}

// Valid reports whether requestID is canonical and has the expected kind.
func (requestID RequestID) Valid() bool {
	return requestID.Validate() == nil
}

// Validate checks requestID without reparsing its textual form.
func (requestID RequestID) Validate() error {
	if requestID.IsZero() {
		return invalidRequestID(ErrInvalidRequestID, "")
	}
	_, err := RequestIDFromID(requestID.value)
	return err
}

// Time returns the embedded UUIDv7 timestamp.
func (requestID RequestID) Time() (time.Time, bool) {
	return requestID.value.Time()
}

func (requestID RequestID) MarshalText() ([]byte, error) {
	if requestID.IsZero() {
		return []byte{}, nil
	}
	if err := requestID.Validate(); err != nil {
		return nil, err
	}
	return []byte(requestID.String()), nil
}

func (requestID *RequestID) UnmarshalText(value []byte) error {
	if requestID == nil {
		return invalidRequestID(ErrInvalidRequestID, string(value))
	}
	if len(value) == 0 {
		*requestID = RequestID{}
		return nil
	}
	parsed, err := ParseRequestID(string(value))
	if err != nil {
		return err
	}
	*requestID = parsed
	return nil
}

func (requestID RequestID) MarshalJSON() ([]byte, error) {
	if requestID.IsZero() {
		return []byte("null"), nil
	}
	if err := requestID.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(requestID.String())
}

func (requestID *RequestID) UnmarshalJSON(value []byte) error {
	if requestID == nil {
		return invalidRequestID(ErrInvalidRequestID, string(value))
	}
	if bytes.Equal(value, []byte("null")) {
		*requestID = RequestID{}
		return nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return invalidRequestID(errors.Join(ErrInvalidRequestID, err), string(value))
	}
	return requestID.UnmarshalText([]byte(text))
}

// Value implements driver.Valuer. The zero value becomes SQL NULL.
func (requestID RequestID) Value() (driver.Value, error) {
	if requestID.IsZero() {
		return nil, nil
	}
	if err := requestID.Validate(); err != nil {
		return nil, err
	}
	return requestID.String(), nil
}

// Scan implements sql.Scanner for textual request IDs.
func (requestID *RequestID) Scan(value any) error {
	if requestID == nil {
		return invalidRequestID(ErrInvalidRequestID, "")
	}
	switch typed := value.(type) {
	case nil:
		*requestID = RequestID{}
		return nil
	case string:
		return requestID.UnmarshalText([]byte(typed))
	case []byte:
		return requestID.UnmarshalText(typed)
	default:
		return invalidRequestID(
			errors.Join(ErrInvalidRequestID, fmt.Errorf("unsupported SQL source type %T", value)),
			"",
		)
	}
}

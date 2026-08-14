// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package identifiers

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	IDSeparator     = '_'
	IDPayloadLength = UUIDCompactLength
	MaximumIDLength = MaximumKindLength + 1 + IDPayloadLength
	MinimumIDLength = MinimumKindLength + 1 + IDPayloadLength
)

// ID is a canonical Mindclade resource identifier:
//
//	<kind>_<32 lowercase hexadecimal UUIDv7 characters>
//
// Examples include "run_019c..." and "model_019c...". The representation is
// lexicographically ordered by the UUIDv7 timestamp for IDs of the same kind.
type ID struct {
	kind Kind
	uuid UUID
}

// NewID creates a resource ID using the package-level UUIDv7 generator.
func NewID(kind Kind) (ID, error) {
	return defaultGenerator.ID(kind)
}

// NewIDAt creates a resource ID using timestamp and the package-level
// monotonic generator.
func NewIDAt(kind Kind, timestamp time.Time) (ID, error) {
	return defaultGenerator.IDAt(kind, timestamp)
}

// IDFromUUID constructs an ID from an RFC variant UUIDv7 value.
func IDFromUUID(kind Kind, uuid UUID) (ID, error) {
	if err := kind.Validate(); err != nil {
		return ID{}, err
	}
	if uuid.Version() != 7 || uuid.Variant() != VariantRFC4122 {
		return ID{}, invalidValue(
			"id uuid",
			uuid.String(),
			"resource IDs require an RFC variant version 7 UUID",
			ErrInvalidID,
			ErrInvalidUUID,
		)
	}
	return ID{kind: kind, uuid: uuid}, nil
}

// ParseID parses the canonical ID format. Unlike ParseUUID, it intentionally
// rejects uppercase hexadecimal payloads so databases and signatures have one
// byte-for-byte representation.
func ParseID(value string) (ID, error) {
	if len(value) < MinimumIDLength || len(value) > MaximumIDLength {
		return ID{}, invalidValue(
			"id",
			value,
			"unexpected length",
			ErrInvalidID,
		)
	}

	separator := strings.IndexByte(value, IDSeparator)
	if separator < MinimumKindLength || separator > MaximumKindLength {
		return ID{}, invalidValue(
			"id",
			value,
			"missing or misplaced kind separator",
			ErrInvalidID,
		)
	}
	if strings.IndexByte(value[separator+1:], IDSeparator) >= 0 {
		return ID{}, invalidValue(
			"id",
			value,
			"contains more than one separator",
			ErrInvalidID,
		)
	}

	kind, err := ParseKind(value[:separator])
	if err != nil {
		return ID{}, invalidValue(
			"id",
			value,
			"invalid kind prefix",
			ErrInvalidID,
			ErrInvalidKind,
		)
	}

	payload := value[separator+1:]
	if len(payload) != IDPayloadLength || payload != strings.ToLower(payload) {
		return ID{}, invalidValue(
			"id",
			value,
			"payload must contain exactly 32 lowercase hexadecimal characters",
			ErrInvalidID,
		)
	}

	uuid, err := ParseUUID(payload)
	if err != nil {
		return ID{}, invalidValue(
			"id",
			value,
			"invalid UUID payload",
			ErrInvalidID,
			ErrInvalidUUID,
		)
	}
	return IDFromUUID(kind, uuid)
}

// ParseIDKind parses value and verifies its expected kind.
func ParseIDKind(value string, expected Kind) (ID, error) {
	if err := expected.Validate(); err != nil {
		return ID{}, err
	}
	identifier, err := ParseID(value)
	if err != nil {
		return ID{}, err
	}
	if identifier.kind != expected {
		return ID{}, invalidValue(
			"id",
			value,
			fmt.Sprintf("expected kind %q, received %q", expected, identifier.kind),
			ErrInvalidID,
		)
	}
	return identifier, nil
}

// MustParseID is ParseID for package-level constants and fixtures. It panics
// on invalid input.
func MustParseID(value string) ID {
	identifier, err := ParseID(value)
	if err != nil {
		panic(err)
	}
	return identifier
}

// String returns the canonical ID, or an empty string for the zero value.
func (identifier ID) String() string {
	if identifier.IsZero() {
		return ""
	}
	return identifier.kind.String() + string(IDSeparator) + identifier.uuid.Compact()
}

// Kind returns the resource prefix.
func (identifier ID) Kind() Kind {
	return identifier.kind
}

// UUID returns the embedded UUIDv7 value.
func (identifier ID) UUID() UUID {
	return identifier.uuid
}

// Time returns the UUIDv7 timestamp.
func (identifier ID) Time() (time.Time, bool) {
	if identifier.IsZero() {
		return time.Time{}, false
	}
	return identifier.uuid.Time()
}

// IsZero reports whether identifier is absent.
func (identifier ID) IsZero() bool {
	return identifier.kind == "" && identifier.uuid.IsZero()
}

// Valid reports whether identifier has a canonical non-zero representation.
func (identifier ID) Valid() bool {
	return identifier.Validate() == nil
}

// Validate checks identifier without reparsing its string form.
func (identifier ID) Validate() error {
	if identifier.IsZero() {
		return invalidValue("id", "", "zero value is not a resource ID", ErrInvalidID)
	}
	if err := identifier.kind.Validate(); err != nil {
		return invalidValue("id", identifier.String(), "invalid kind", ErrInvalidID, ErrInvalidKind)
	}
	if identifier.uuid.Version() != 7 || identifier.uuid.Variant() != VariantRFC4122 {
		return invalidValue(
			"id",
			identifier.String(),
			"embedded UUID must be RFC variant version 7",
			ErrInvalidID,
			ErrInvalidUUID,
		)
	}
	return nil
}

// Compare orders IDs by kind and then UUID bytes.
func (identifier ID) Compare(other ID) int {
	if comparison := strings.Compare(identifier.kind.String(), other.kind.String()); comparison != 0 {
		return comparison
	}
	return identifier.uuid.Compare(other.uuid)
}

// Less reports whether identifier sorts before other.
func (identifier ID) Less(other ID) bool {
	return identifier.Compare(other) < 0
}

func (identifier ID) MarshalText() ([]byte, error) {
	if identifier.IsZero() {
		return []byte{}, nil
	}
	if err := identifier.Validate(); err != nil {
		return nil, err
	}
	return []byte(identifier.String()), nil
}

func (identifier *ID) UnmarshalText(value []byte) error {
	if identifier == nil {
		return invalidValue("id", string(value), "nil destination", ErrInvalidID)
	}
	if len(value) == 0 {
		*identifier = ID{}
		return nil
	}
	parsed, err := ParseID(string(value))
	if err != nil {
		return err
	}
	*identifier = parsed
	return nil
}

func (identifier ID) MarshalJSON() ([]byte, error) {
	if identifier.IsZero() {
		return []byte("null"), nil
	}
	if err := identifier.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(identifier.String())
}

func (identifier *ID) UnmarshalJSON(value []byte) error {
	if identifier == nil {
		return invalidValue("id", string(value), "nil destination", ErrInvalidID)
	}
	if bytes.Equal(value, []byte("null")) {
		*identifier = ID{}
		return nil
	}

	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return invalidValue("id", string(value), "expected a JSON string or null", ErrInvalidID)
	}
	return identifier.UnmarshalText([]byte(text))
}

// Value implements driver.Valuer. The zero value becomes SQL NULL.
func (identifier ID) Value() (driver.Value, error) {
	if identifier.IsZero() {
		return nil, nil
	}
	if err := identifier.Validate(); err != nil {
		return nil, err
	}
	return identifier.String(), nil
}

// Scan implements sql.Scanner for textual IDs.
func (identifier *ID) Scan(value any) error {
	if identifier == nil {
		return invalidValue("id", "", "nil destination", ErrInvalidID)
	}

	switch typed := value.(type) {
	case nil:
		*identifier = ID{}
		return nil
	case string:
		return identifier.UnmarshalText([]byte(typed))
	case []byte:
		return identifier.UnmarshalText(typed)
	default:
		return invalidValue(
			"id",
			fmt.Sprintf("%T", value),
			"unsupported SQL source type",
			ErrInvalidID,
		)
	}
}

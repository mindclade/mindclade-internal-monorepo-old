// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package identifiers

import (
	"bytes"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	UUIDBinaryLength  = 16
	UUIDCompactLength = 32
	UUIDStringLength  = 36
)

// UUID is a 128-bit universally unique identifier.
type UUID [UUIDBinaryLength]byte

// Variant describes the UUID variant encoded in the most significant bits of
// byte 8.
type Variant uint8

const (
	VariantNCS Variant = iota
	VariantRFC4122
	VariantMicrosoft
	VariantFuture
)

// ParseUUID accepts canonical 8-4-4-4-12 UUID text, compact 32-character
// hexadecimal text, and the RFC URN form "urn:uuid:<uuid>". Hexadecimal input
// is case-insensitive; String always emits lowercase canonical text.
func ParseUUID(value string) (UUID, error) {
	original := value
	if strings.HasPrefix(strings.ToLower(value), "urn:uuid:") {
		value = value[len("urn:uuid:"):]
	}

	var compact string
	switch len(value) {
	case UUIDCompactLength:
		compact = value
	case UUIDStringLength:
		if value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
			return UUID{}, invalidValue(
				"uuid",
				original,
				"hyphens must use the canonical 8-4-4-4-12 layout",
				ErrInvalidUUID,
			)
		}
		compact = value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:]
	default:
		return UUID{}, invalidValue(
			"uuid",
			original,
			"expected 32 hexadecimal characters or canonical 8-4-4-4-12 text",
			ErrInvalidUUID,
		)
	}

	decoded, err := hex.DecodeString(compact)
	if err != nil {
		return UUID{}, invalidValue(
			"uuid",
			original,
			"contains non-hexadecimal characters",
			ErrInvalidUUID,
		)
	}

	var uuid UUID
	copy(uuid[:], decoded)
	return uuid, nil
}

// MustParseUUID is ParseUUID for package-level constants and fixtures. It
// panics on invalid input.
func MustParseUUID(value string) UUID {
	uuid, err := ParseUUID(value)
	if err != nil {
		panic(err)
	}
	return uuid
}

// UUIDFromBytes constructs a UUID from exactly 16 bytes.
func UUIDFromBytes(value []byte) (UUID, error) {
	if len(value) != UUIDBinaryLength {
		return UUID{}, invalidValue(
			"uuid bytes",
			fmt.Sprintf("length=%d", len(value)),
			"expected exactly 16 bytes",
			ErrInvalidUUID,
		)
	}
	var uuid UUID
	copy(uuid[:], value)
	return uuid, nil
}

// String returns canonical lowercase 8-4-4-4-12 UUID text.
func (uuid UUID) String() string {
	var output [UUIDStringLength]byte
	hex.Encode(output[0:8], uuid[0:4])
	output[8] = '-'
	hex.Encode(output[9:13], uuid[4:6])
	output[13] = '-'
	hex.Encode(output[14:18], uuid[6:8])
	output[18] = '-'
	hex.Encode(output[19:23], uuid[8:10])
	output[23] = '-'
	hex.Encode(output[24:36], uuid[10:16])
	return string(output[:])
}

// Compact returns lowercase UUID text without hyphens.
func (uuid UUID) Compact() string {
	var output [UUIDCompactLength]byte
	hex.Encode(output[:], uuid[:])
	return string(output[:])
}

// URN returns the RFC UUID URN representation.
func (uuid UUID) URN() string {
	return "urn:uuid:" + uuid.String()
}

// Bytes returns an independent copy of the 16 UUID bytes.
func (uuid UUID) Bytes() []byte {
	output := make([]byte, UUIDBinaryLength)
	copy(output, uuid[:])
	return output
}

// IsZero reports whether uuid is the nil UUID.
func (uuid UUID) IsZero() bool {
	return uuid == UUID{}
}

// Version returns the four-bit UUID version.
func (uuid UUID) Version() uint8 {
	return uuid[6] >> 4
}

// Variant returns the UUID variant.
func (uuid UUID) Variant() Variant {
	switch {
	case uuid[8]&0x80 == 0:
		return VariantNCS
	case uuid[8]&0xC0 == 0x80:
		return VariantRFC4122
	case uuid[8]&0xE0 == 0xC0:
		return VariantMicrosoft
	default:
		return VariantFuture
	}
}

// Time returns the embedded Unix-millisecond timestamp for a version 7 UUID.
func (uuid UUID) Time() (time.Time, bool) {
	if uuid.Version() != 7 || uuid.Variant() != VariantRFC4122 {
		return time.Time{}, false
	}

	milliseconds := int64(uuid[0])<<40 |
		int64(uuid[1])<<32 |
		int64(uuid[2])<<24 |
		int64(uuid[3])<<16 |
		int64(uuid[4])<<8 |
		int64(uuid[5])
	return time.UnixMilli(milliseconds).UTC(), true
}

// Compare returns -1, 0, or 1 according to UUID byte ordering.
func (uuid UUID) Compare(other UUID) int {
	return bytes.Compare(uuid[:], other[:])
}

// Less reports whether uuid sorts before other.
func (uuid UUID) Less(other UUID) bool {
	return uuid.Compare(other) < 0
}

func (uuid UUID) MarshalText() ([]byte, error) {
	return []byte(uuid.String()), nil
}

func (uuid *UUID) UnmarshalText(value []byte) error {
	if uuid == nil {
		return invalidValue("uuid", string(value), "nil destination", ErrInvalidUUID)
	}
	parsed, err := ParseUUID(string(value))
	if err != nil {
		return err
	}
	*uuid = parsed
	return nil
}

func (uuid UUID) MarshalJSON() ([]byte, error) {
	return json.Marshal(uuid.String())
}

func (uuid *UUID) UnmarshalJSON(value []byte) error {
	if uuid == nil {
		return invalidValue("uuid", string(value), "nil destination", ErrInvalidUUID)
	}
	if bytes.Equal(value, []byte("null")) {
		*uuid = UUID{}
		return nil
	}

	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return invalidValue("uuid", string(value), "expected a JSON string", ErrInvalidUUID)
	}
	return uuid.UnmarshalText([]byte(text))
}

func (uuid UUID) MarshalBinary() ([]byte, error) {
	return uuid.Bytes(), nil
}

func (uuid *UUID) UnmarshalBinary(value []byte) error {
	if uuid == nil {
		return invalidValue("uuid bytes", "", "nil destination", ErrInvalidUUID)
	}
	parsed, err := UUIDFromBytes(value)
	if err != nil {
		return err
	}
	*uuid = parsed
	return nil
}

// Value implements driver.Valuer using canonical text.
func (uuid UUID) Value() (driver.Value, error) {
	return uuid.String(), nil
}

// Scan implements sql.Scanner. It accepts canonical or compact text and raw
// 16-byte binary UUID values.
func (uuid *UUID) Scan(value any) error {
	if uuid == nil {
		return invalidValue("uuid", "", "nil destination", ErrInvalidUUID)
	}

	switch typed := value.(type) {
	case nil:
		*uuid = UUID{}
		return nil
	case string:
		return uuid.UnmarshalText([]byte(typed))
	case []byte:
		if len(typed) == UUIDBinaryLength {
			return uuid.UnmarshalBinary(typed)
		}
		return uuid.UnmarshalText(typed)
	default:
		return invalidValue(
			"uuid",
			fmt.Sprintf("%T", value),
			"unsupported SQL source type",
			ErrInvalidUUID,
		)
	}
}

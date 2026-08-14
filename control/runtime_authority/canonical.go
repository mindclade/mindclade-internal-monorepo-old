// Copyright 2026 Mindclade. All rights reserved.
package runtime_authority

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// canonicalEncoder implements the Mindclade Canonical Claims Encoding v1
// (MCCE1). It is intentionally simple enough to reproduce in Go, Rust, and
// Python without depending on language-specific JSON or protobuf serializers.
// Each document starts with an ASCII type name followed by a NUL byte. Fields
// are encoded as: u16 key length, key bytes, u32 value length, value bytes.
// Unsigned integers use big-endian fixed-width encoding. Set-like lists are
// validated as strictly sorted and unique before encoding.
//
// The encoder never panics or truncates lengths. The first encoding error is
// retained and returned by bytes().
type canonicalEncoder struct {
	bytes.Buffer
	err error
}

func newCanonicalEncoder(documentType string) *canonicalEncoder {
	enc := &canonicalEncoder{}
	if documentType == "" || len(documentType) > 128 {
		enc.err = invalid("canonical_document_type_invalid", "canonical document type is invalid", nil)
		return enc
	}
	enc.WriteString("MCCE1/")
	enc.WriteString(documentType)
	enc.WriteByte(0)
	return enc
}

func (enc *canonicalEncoder) field(key string, value []byte) {
	if enc.err != nil {
		return
	}
	if key == "" || len(key) > math.MaxUint16 {
		enc.err = invalid("canonical_field_key_invalid", "canonical field key is invalid", nil)
		return
	}
	if uint64(len(value)) > math.MaxUint32 {
		enc.err = invalid("canonical_field_value_too_large", "canonical field value exceeds uint32 framing", nil)
		return
	}
	var keyLen [2]byte
	binary.BigEndian.PutUint16(keyLen[:], uint16(len(key)))
	enc.Write(keyLen[:])
	enc.WriteString(key)
	var valueLen [4]byte
	binary.BigEndian.PutUint32(valueLen[:], uint32(len(value)))
	enc.Write(valueLen[:])
	enc.Write(value)
}

func (enc *canonicalEncoder) text(key, value string) { enc.field(key, []byte(value)) }

func (enc *canonicalEncoder) u64(key string, value uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], value)
	enc.field(key, b[:])
}

func (enc *canonicalEncoder) u32(key string, value uint32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], value)
	enc.field(key, b[:])
}

func (enc *canonicalEncoder) boolean(key string, value bool) {
	if value {
		enc.field(key, []byte{1})
	} else {
		enc.field(key, []byte{0})
	}
}

func (enc *canonicalEncoder) stringSet(key string, values []string) {
	if enc.err != nil {
		return
	}
	if uint64(len(values)) > math.MaxUint32 {
		enc.err = invalid("canonical_set_too_large", "canonical string set exceeds uint32 framing", nil)
		return
	}
	if err := requireSortedUnique(values, key); err != nil {
		enc.err = err
		return
	}
	var b bytes.Buffer
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], uint32(len(values)))
	b.Write(count[:])
	for _, value := range values {
		if uint64(len(value)) > math.MaxUint32 {
			enc.err = invalid("canonical_set_value_too_large", "canonical string-set value exceeds uint32 framing", nil)
			return
		}
		var valueLen [4]byte
		binary.BigEndian.PutUint32(valueLen[:], uint32(len(value)))
		b.Write(valueLen[:])
		b.WriteString(value)
	}
	enc.field(key, b.Bytes())
}

func (enc *canonicalEncoder) nested(key string, payload []byte) { enc.field(key, payload) }

func (enc *canonicalEncoder) bytes() ([]byte, error) {
	if enc.err != nil {
		return nil, enc.err
	}
	return append([]byte(nil), enc.Buffer.Bytes()...), nil
}

func unixMillis(value time.Time, field string) (uint64, error) {
	if value.IsZero() {
		return 0, invalid("canonical_time_required", field+" is required", nil)
	}
	millis := value.UTC().UnixMilli()
	if millis < 0 {
		return 0, invalid("canonical_time_before_epoch", field+" must not be before Unix epoch", nil)
	}
	return uint64(millis), nil
}

func cloneStrings(values []string) []string { return append([]string(nil), values...) }

func requireSortedUnique(values []string, field string) error {
	for i, value := range values {
		if value == "" {
			return invalid("empty_"+field, fmt.Sprintf("%s contains an empty value", field), nil)
		}
		if i > 0 && values[i-1] >= value {
			return invalid("noncanonical_"+field, fmt.Sprintf("%s must be strictly sorted and unique", field), nil)
		}
	}
	return nil
}

// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package evidence

import (
	"bytes"
	"encoding/binary"
	"math"
	"time"
)

type canonicalEncoder struct {
	bytes.Buffer
	err error
}

func newCanonicalEncoder(documentType string) *canonicalEncoder {
	encoder := &canonicalEncoder{}
	if documentType == "" || len(documentType) > 128 {
		encoder.err = invalid("canonical_document_type_invalid", "canonical document type is invalid", nil)
		return encoder
	}
	encoder.WriteString("MCCE1/")
	encoder.WriteString(documentType)
	encoder.WriteByte(0)
	return encoder
}

func (encoder *canonicalEncoder) field(key string, value []byte) {
	if encoder.err != nil {
		return
	}
	if key == "" || len(key) > math.MaxUint16 || uint64(len(value)) > math.MaxUint32 {
		encoder.err = invalid("canonical_field_invalid", "canonical field is invalid", nil)
		return
	}
	var keyLength [2]byte
	binary.BigEndian.PutUint16(keyLength[:], uint16(len(key)))
	encoder.Write(keyLength[:])
	encoder.WriteString(key)
	var valueLength [4]byte
	binary.BigEndian.PutUint32(valueLength[:], uint32(len(value)))
	encoder.Write(valueLength[:])
	encoder.Write(value)
}

func (encoder *canonicalEncoder) text(key, value string) { encoder.field(key, []byte(value)) }

func (encoder *canonicalEncoder) u64(key string, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	encoder.field(key, encoded[:])
}

func (encoder *canonicalEncoder) timestamp(key string, value time.Time) {
	if value.IsZero() || value.UnixMilli() < 0 {
		encoder.err = invalid("canonical_time_invalid", "canonical timestamp is invalid", nil)
		return
	}
	encoder.u64(key, uint64(value.UTC().UnixMilli()))
}

func (encoder *canonicalEncoder) strings(key string, values []string) {
	if encoder.err != nil {
		return
	}
	var payload bytes.Buffer
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], uint32(len(values)))
	payload.Write(count[:])
	for index, value := range values {
		if value == "" || index > 0 && values[index-1] >= value || uint64(len(value)) > math.MaxUint32 {
			encoder.err = invalid("canonical_string_set_invalid", "canonical string set must be sorted and unique", nil)
			return
		}
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		payload.Write(length[:])
		payload.WriteString(value)
	}
	encoder.field(key, payload.Bytes())
}

func (encoder *canonicalEncoder) nested(key string, value []byte) { encoder.field(key, value) }

func (encoder *canonicalEncoder) bytes() ([]byte, error) {
	if encoder.err != nil {
		return nil, encoder.err
	}
	return append([]byte(nil), encoder.Bytes()...), nil
}

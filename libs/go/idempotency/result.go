// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package idempotency

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

const (
	MaximumResultBytes         = 16 * 1024 * 1024
	MaximumResultMetadata      = 64
	MaximumResultMetadataKey   = 64
	MaximumResultMetadataValue = 4096
	MaximumContentTypeLength   = 256
)

// Result is an opaque replay payload plus bounded metadata. The presence bit
// lets an empty successful payload remain distinct from the zero value.
type Result struct {
	present     bool
	payload     []byte
	digest      identifiers.Digest
	contentType string
	metadata    map[string]string
}

func NewResult(payload []byte, contentType string, metadata map[string]string) (Result, error) {
	normalizedMetadata, err := normalizeMetadata(metadata)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		present:     true,
		payload:     append([]byte(nil), payload...),
		digest:      identifiers.SHA256(payload),
		contentType: strings.TrimSpace(contentType),
		metadata:    normalizedMetadata,
	}
	if err := result.Validate(); err != nil {
		return Result{}, err
	}
	return result, nil
}
func EmptyResult() (Result, error)                { return NewResult(nil, "", nil) }
func (result Result) IsZero() bool                { return !result.present }
func (result Result) Payload() []byte             { return append([]byte(nil), result.payload...) }
func (result Result) Digest() identifiers.Digest  { return result.digest }
func (result Result) ContentType() string         { return result.contentType }
func (result Result) Metadata() map[string]string { return cloneMetadata(result.metadata) }

// Equal reports whether two results contain the same replay payload and
// metadata. Payload comparison is exact; the digest is also compared so a
// malformed in-memory value cannot compare equal accidentally.
func (result Result) Equal(other Result) bool {
	if result.present != other.present {
		return false
	}
	if !result.present {
		return true
	}
	if !result.digest.Equal(other.digest) ||
		result.contentType != other.contentType ||
		!bytes.Equal(result.payload, other.payload) ||
		len(result.metadata) != len(other.metadata) {
		return false
	}
	for key, value := range result.metadata {
		if other.metadata[key] != value {
			return false
		}
	}
	return true
}

func (result Result) Validate() error {
	if !result.present {
		return invalid(ErrInvalidResult, ReasonInvalidResult, "invalid idempotency result", "idempotency.Result.Validate", nil)
	}
	if len(result.payload) > MaximumResultBytes {
		return invalid(ErrInvalidResult, ReasonInvalidResult, "idempotency result is too large", "idempotency.Result.Validate", faults.Fields{"maximum_bytes": MaximumResultBytes})
	}
	if !result.digest.Valid() || !result.digest.Equal(identifiers.SHA256(result.payload)) {
		return invalid(ErrInvalidResult, ReasonInvalidResult, "idempotency result digest is invalid", "idempotency.Result.Validate", nil)
	}
	if len(result.contentType) > MaximumContentTypeLength || !utf8.ValidString(result.contentType) {
		return invalid(ErrInvalidResult, ReasonInvalidResult, "idempotency result content type is invalid", "idempotency.Result.Validate", nil)
	}
	for _, character := range result.contentType {
		if unicode.IsControl(character) {
			return invalid(ErrInvalidResult, ReasonInvalidResult, "idempotency result content type is invalid", "idempotency.Result.Validate", nil)
		}
	}
	return validateMetadata(result.metadata)
}

type resultJSON struct {
	Payload     []byte            `json:"payload"`
	Digest      string            `json:"digest"`
	ContentType string            `json:"content_type,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

func (result Result) MarshalJSON() ([]byte, error) {
	if result.IsZero() {
		return []byte("null"), nil
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(resultJSON{Payload: result.Payload(), Digest: result.digest.String(), ContentType: result.contentType, Metadata: result.Metadata()})
}
func (result *Result) UnmarshalJSON(value []byte) error {
	if result == nil {
		return invalid(ErrInvalidResult, ReasonInvalidResult, "invalid idempotency result", "idempotency.Result.UnmarshalJSON", nil)
	}
	if bytes.Equal(value, []byte("null")) {
		*result = Result{}
		return nil
	}
	var wire resultJSON
	if err := json.Unmarshal(value, &wire); err != nil {
		return invalid(errors.Join(ErrInvalidResult, err), ReasonInvalidResult, "invalid idempotency result", "idempotency.Result.UnmarshalJSON", nil)
	}
	wireDigest, err := identifiers.ParseDigest(wire.Digest)
	if err != nil {
		return invalid(errors.Join(ErrInvalidResult, err), ReasonInvalidResult, "idempotency result digest is missing or invalid", "idempotency.Result.UnmarshalJSON", nil)
	}
	parsed, err := NewResult(wire.Payload, wire.ContentType, wire.Metadata)
	if err != nil {
		return err
	}
	if !parsed.digest.Equal(wireDigest) {
		return invalid(ErrInvalidResult, ReasonInvalidResult, "idempotency result digest does not match payload", "idempotency.Result.UnmarshalJSON", nil)
	}
	*result = parsed
	return nil
}

func cloneMetadata(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
func normalizeMetadata(metadata map[string]string) (map[string]string, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	if len(metadata) > MaximumResultMetadata {
		return nil, invalid(ErrInvalidResult, ReasonInvalidResult, "idempotency result has too much metadata", "idempotency.Result.Validate", nil)
	}
	normalized := make(map[string]string, len(metadata))
	for rawKey, value := range metadata {
		key := strings.TrimSpace(rawKey)
		if err := validateMetadataEntry(key, value); err != nil {
			return nil, err
		}
		if _, exists := normalized[key]; exists {
			return nil, invalid(ErrInvalidResult, ReasonInvalidResult, "idempotency result metadata contains duplicate canonical keys", "idempotency.Result.Validate", faults.Fields{"metadata_key": key})
		}
		normalized[key] = value
	}
	return normalized, nil
}

func validateMetadata(metadata map[string]string) error {
	if len(metadata) > MaximumResultMetadata {
		return invalid(ErrInvalidResult, ReasonInvalidResult, "idempotency result has too much metadata", "idempotency.Result.Validate", nil)
	}
	for key, value := range metadata {
		if key != strings.TrimSpace(key) {
			return invalid(ErrInvalidResult, ReasonInvalidResult, "idempotency result metadata is not canonical", "idempotency.Result.Validate", faults.Fields{"metadata_key": strings.TrimSpace(key)})
		}
		if err := validateMetadataEntry(key, value); err != nil {
			return err
		}
	}
	return nil
}

func validateMetadataEntry(key, value string) error {
	if key == "" || len(key) > MaximumResultMetadataKey || len(value) > MaximumResultMetadataValue || !utf8.ValidString(value) {
		return invalid(ErrInvalidResult, ReasonInvalidResult, "idempotency result metadata is invalid", "idempotency.Result.Validate", faults.Fields{"metadata_key": key})
	}
	previousSeparator := false
	for index := 0; index < len(key); index++ {
		character := key[index]
		isLetter := character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		isSeparator := character == '_' || character == '-' || character == '.'
		if !isLetter && !isDigit && !isSeparator ||
			index == 0 && !isLetter && !isDigit ||
			index == len(key)-1 && isSeparator ||
			isSeparator && previousSeparator {
			return invalid(ErrInvalidResult, ReasonInvalidResult, "idempotency result metadata is invalid", "idempotency.Result.Validate", faults.Fields{"metadata_key": key})
		}
		previousSeparator = isSeparator
	}
	canonical := strings.NewReplacer("-", "_", ".", "_", ":", "_", "/", "_").Replace(strings.ToLower(key))
	for _, marker := range []string{"secret", "password", "token", "credential", "private_key", "api_key", "authorization", "cookie"} {
		if strings.Contains(canonical, marker) {
			return invalid(ErrInvalidResult, ReasonInvalidResult, "sensitive idempotency result metadata is forbidden", "idempotency.Result.Validate", faults.Fields{"metadata_key": key})
		}
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return invalid(ErrInvalidResult, ReasonInvalidResult, "idempotency result metadata is invalid", "idempotency.Result.Validate", faults.Fields{"metadata_key": key})
		}
	}
	return nil
}

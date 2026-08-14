// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package blob

import (
	"sort"
	"strings"
	"unicode"
)

const (
	MaximumMetadataEntries     = 64
	MaximumMetadataKeyLength   = 128
	MaximumMetadataValueLength = 2048
)

type Metadata map[string]string

func NewMetadata(values map[string]string) (Metadata, error) {
	metadata := cloneMetadata(values)
	if err := metadata.Validate(); err != nil {
		return nil, err
	}
	return metadata, nil
}

func (metadata Metadata) Validate() error {
	if len(metadata) > MaximumMetadataEntries {
		return invalidArgument(nil, ErrInvalidMetadata, "invalid blob metadata", "too_many_blob_metadata_entries", "blob.Metadata.Validate", nil)
	}
	for key, value := range metadata {
		if !validMetadataKey(key) || len(value) > MaximumMetadataValueLength || strings.TrimSpace(value) != value {
			return invalidArgument(nil, ErrInvalidMetadata, "invalid blob metadata", "invalid_blob_metadata", "blob.Metadata.Validate", nil)
		}
		for _, character := range value {
			if character == 0 || unicode.IsControl(character) {
				return invalidArgument(nil, ErrInvalidMetadata, "invalid blob metadata", "invalid_blob_metadata_value", "blob.Metadata.Validate", nil)
			}
		}
	}
	return nil
}

func (metadata Metadata) Clone() Metadata { return cloneMetadata(metadata) }

func (metadata Metadata) Keys() []string {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func validMetadataKey(key string) bool {
	if key == "" || len(key) > MaximumMetadataKeyLength || strings.TrimSpace(key) != key || key != strings.ToLower(key) {
		return false
	}
	previousSeparator := false
	for index := 0; index < len(key); index++ {
		character := key[index]
		letter := character >= 'a' && character <= 'z'
		digit := character >= '0' && character <= '9'
		separator := character == '.' || character == '_' || character == '-'
		if !letter && !digit && !separator || index == 0 && !letter || index == len(key)-1 && separator || separator && previousSeparator {
			return false
		}
		previousSeparator = separator
	}
	return true
}

func cloneMetadata(values map[string]string) Metadata {
	if len(values) == 0 {
		return nil
	}
	clone := make(Metadata, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

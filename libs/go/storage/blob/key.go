// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package blob

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaximumKeyLength = 1024

type Key string

func ParseKey(value string) (Key, error) {
	key := Key(value)
	if err := key.Validate(); err != nil {
		return "", err
	}
	return key, nil
}

func MustParseKey(value string) Key {
	key, err := ParseKey(value)
	if err != nil {
		panic(err)
	}
	return key
}

func (key Key) String() string { return string(key) }
func (key Key) IsZero() bool   { return key == "" }
func (key Key) Valid() bool    { return key.Validate() == nil }

func (key Key) Validate() error {
	value := string(key)
	if value == "" || len(value) > MaximumKeyLength || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return invalidArgument(nil, ErrInvalidKey, "invalid blob key", "invalid_blob_key", "blob.Key.Validate", nil)
	}
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "\\") || strings.Contains(value, "//") {
		return invalidArgument(nil, ErrInvalidKey, "invalid blob key", "non_canonical_blob_key", "blob.Key.Validate", nil)
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return invalidArgument(nil, ErrInvalidKey, "invalid blob key", "non_canonical_blob_key", "blob.Key.Validate", nil)
		}
	}
	for _, character := range value {
		if character == 0 || unicode.IsControl(character) {
			return invalidArgument(nil, ErrInvalidKey, "invalid blob key", "invalid_blob_key_character", "blob.Key.Validate", nil)
		}
	}
	return nil
}

func (key Key) MarshalText() ([]byte, error) {
	if key.IsZero() {
		return []byte{}, nil
	}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return []byte(key), nil
}

func (key *Key) UnmarshalText(value []byte) error {
	if key == nil {
		return invalidArgument(nil, ErrInvalidKey, "invalid blob key", "nil_blob_key_destination", "blob.Key.UnmarshalText", nil)
	}
	if len(value) == 0 {
		*key = ""
		return nil
	}
	parsed, err := ParseKey(string(value))
	if err != nil {
		return err
	}
	*key = parsed
	return nil
}

func (key Key) MarshalJSON() ([]byte, error) {
	if key.IsZero() {
		return []byte("null"), nil
	}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(string(key))
}

func (key *Key) UnmarshalJSON(value []byte) error {
	if key == nil {
		return invalidArgument(nil, ErrInvalidKey, "invalid blob key", "nil_blob_key_destination", "blob.Key.UnmarshalJSON", nil)
	}
	if string(value) == "null" {
		*key = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return invalidArgument(nil, err, "invalid blob key", "invalid_blob_key_json", "blob.Key.UnmarshalJSON", nil)
	}
	return key.UnmarshalText([]byte(text))
}

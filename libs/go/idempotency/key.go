// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package idempotency

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
)

const (
	MinimumKeyLength   = 8
	MaximumKeyLength   = 200
	MinimumScopeLength = 3
	MaximumScopeLength = 256
)

// Key is a bounded visible-ASCII client token. Its zero value is absent.
type Key struct{ value string }

func ParseKey(value string) (Key, error) {
	normalized := strings.TrimSpace(value)
	key := Key{value: normalized}
	if err := key.Validate(); err != nil {
		return Key{}, err
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

func (key Key) String() string { return key.value }
func (key Key) IsZero() bool   { return key.value == "" }
func (key Key) Valid() bool    { return key.Validate() == nil }
func (key Key) Validate() error {
	if len(key.value) < MinimumKeyLength || len(key.value) > MaximumKeyLength {
		return invalid(ErrInvalidKey, ReasonInvalidKey, "invalid idempotency key", "idempotency.Key.Validate", faults.Fields{"key_length": len(key.value)})
	}
	for index := 0; index < len(key.value); index++ {
		character := key.value[index]
		if character < 0x21 || character > 0x7e {
			return invalid(ErrInvalidKey, ReasonInvalidKey, "invalid idempotency key", "idempotency.Key.Validate", faults.Fields{"key_length": len(key.value)})
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
	return []byte(key.value), nil
}
func (key *Key) UnmarshalText(value []byte) error {
	if key == nil {
		return invalid(ErrInvalidKey, ReasonInvalidKey, "invalid idempotency key", "idempotency.Key.UnmarshalText", nil)
	}
	if len(value) == 0 {
		*key = Key{}
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
	return json.Marshal(key.value)
}
func (key *Key) UnmarshalJSON(value []byte) error {
	if key == nil {
		return invalid(ErrInvalidKey, ReasonInvalidKey, "invalid idempotency key", "idempotency.Key.UnmarshalJSON", nil)
	}
	if bytes.Equal(value, []byte("null")) {
		*key = Key{}
		return nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return invalid(errors.Join(ErrInvalidKey, err), ReasonInvalidKey, "invalid idempotency key", "idempotency.Key.UnmarshalJSON", nil)
	}
	return key.UnmarshalText([]byte(text))
}
func (key Key) Value() (driver.Value, error) {
	if key.IsZero() {
		return nil, nil
	}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return key.value, nil
}
func (key *Key) Scan(value any) error {
	if key == nil {
		return ErrInvalidKey
	}
	switch typed := value.(type) {
	case nil:
		*key = Key{}
		return nil
	case string:
		return key.UnmarshalText([]byte(typed))
	case []byte:
		return key.UnmarshalText(typed)
	default:
		return invalid(errors.Join(ErrInvalidKey, fmt.Errorf("unsupported SQL source type %T", value)), ReasonInvalidKey, "invalid idempotency key", "idempotency.Key.Scan", nil)
	}
}

// Scope is a server-controlled namespace such as
// "org_.../control-plane/runs.create". It prevents client keys from colliding
// across organizations, principals, and operations.
type Scope struct{ value string }

func ParseScope(value string) (Scope, error) {
	normalized := strings.TrimSpace(value)
	scope := Scope{value: normalized}
	if err := scope.Validate(); err != nil {
		return Scope{}, err
	}
	return scope, nil
}
func MustParseScope(value string) Scope {
	scope, err := ParseScope(value)
	if err != nil {
		panic(err)
	}
	return scope
}
func (scope Scope) String() string { return scope.value }
func (scope Scope) IsZero() bool   { return scope.value == "" }
func (scope Scope) Valid() bool    { return scope.Validate() == nil }
func (scope Scope) Validate() error {
	if len(scope.value) < MinimumScopeLength || len(scope.value) > MaximumScopeLength || scope.value[0] < 'a' || scope.value[0] > 'z' {
		return invalid(ErrInvalidScope, ReasonInvalidScope, "invalid idempotency scope", "idempotency.Scope.Validate", faults.Fields{"scope": scope.value})
	}
	previousSeparator := false
	for index := 0; index < len(scope.value); index++ {
		character := scope.value[index]
		isLetter := character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		isSeparator := strings.ContainsRune("._:/-", rune(character))
		if !isLetter && !isDigit && !isSeparator || isSeparator && previousSeparator || index == len(scope.value)-1 && isSeparator {
			return invalid(ErrInvalidScope, ReasonInvalidScope, "invalid idempotency scope", "idempotency.Scope.Validate", faults.Fields{"scope": scope.value})
		}
		previousSeparator = isSeparator
	}
	return nil
}
func (scope Scope) MarshalText() ([]byte, error) {
	if scope.IsZero() {
		return []byte{}, nil
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return []byte(scope.value), nil
}
func (scope *Scope) UnmarshalText(value []byte) error {
	if scope == nil {
		return ErrInvalidScope
	}
	if len(value) == 0 {
		*scope = Scope{}
		return nil
	}
	parsed, err := ParseScope(string(value))
	if err != nil {
		return err
	}
	*scope = parsed
	return nil
}
func (scope Scope) MarshalJSON() ([]byte, error) {
	if scope.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(scope.value)
}
func (scope *Scope) UnmarshalJSON(value []byte) error {
	if scope == nil {
		return invalid(ErrInvalidScope, ReasonInvalidScope, "invalid idempotency scope", "idempotency.Scope.UnmarshalJSON", nil)
	}
	if bytes.Equal(value, []byte("null")) {
		*scope = Scope{}
		return nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return err
	}
	return scope.UnmarshalText([]byte(text))
}

// Identity is the full namespace of one client key.
type Identity struct {
	Scope Scope `json:"scope"`
	Key   Key   `json:"key"`
}

func NewIdentity(scope Scope, key Key) (Identity, error) {
	identity := Identity{Scope: scope, Key: key}
	if err := identity.Validate(); err != nil {
		return Identity{}, err
	}
	return identity, nil
}
func (identity Identity) IsZero() bool { return identity.Scope.IsZero() && identity.Key.IsZero() }
func (identity Identity) Valid() bool  { return identity.Validate() == nil }
func (identity Identity) Validate() error {
	if err := identity.Scope.Validate(); err != nil {
		return invalid(errors.Join(ErrInvalidIdentity, err), ReasonInvalidIdentity, "invalid idempotency identity", "idempotency.Identity.Validate", nil)
	}
	if err := identity.Key.Validate(); err != nil {
		return invalid(errors.Join(ErrInvalidIdentity, err), ReasonInvalidIdentity, "invalid idempotency identity", "idempotency.Identity.Validate", nil)
	}
	return nil
}
func (identity Identity) String() string {
	return identity.Scope.String() + "/" + identity.Key.String()
}

// Digest returns a stable storage-safe hash of scope and key.
func (identity Identity) Digest() identifiers.Digest {
	return identifiers.SHA256([]byte(identity.Scope.String() + "\x00" + identity.Key.String()))
}

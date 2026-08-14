// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package requestmeta

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

const MaximumOperationLength = 160

// Operation is a stable logical operation name such as
// "runs.Repository.Create" or "models.release.promote".
type Operation struct {
	value string
}

func ParseOperation(value string) (Operation, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || len(normalized) > MaximumOperationLength {
		return Operation{}, invalidArgument(
			ErrInvalidOperation,
			"invalid operation name",
			"invalid_operation",
			map[string]any{"value_length": len(normalized)},
		)
	}
	previousSeparator := false
	for index := 0; index < len(normalized); index++ {
		character := normalized[index]
		isLetter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		isDigit := character >= '0' && character <= '9'
		isSeparator := strings.ContainsRune("._:/-", rune(character))
		if !isLetter && !isDigit && !isSeparator ||
			index == 0 && !isLetter ||
			index == len(normalized)-1 && isSeparator ||
			isSeparator && previousSeparator {
			return Operation{}, invalidArgument(
				ErrInvalidOperation,
				"invalid operation name",
				"invalid_operation",
				map[string]any{"value_length": len(normalized)},
			)
		}
		previousSeparator = isSeparator
	}
	return Operation{value: normalized}, nil
}

func MustParseOperation(value string) Operation {
	operation, err := ParseOperation(value)
	if err != nil {
		panic(err)
	}
	return operation
}

func (operation Operation) String() string { return operation.value }
func (operation Operation) IsZero() bool   { return operation.value == "" }
func (operation Operation) Valid() bool {
	if operation.IsZero() {
		return false
	}
	_, err := ParseOperation(operation.value)
	return err == nil
}

func (operation Operation) MarshalText() ([]byte, error) {
	if operation.IsZero() {
		return []byte{}, nil
	}
	if !operation.Valid() {
		return nil, invalidArgument(ErrInvalidOperation, "invalid operation name", "invalid_operation", nil)
	}
	return []byte(operation.value), nil
}

func (operation *Operation) UnmarshalText(value []byte) error {
	if operation == nil {
		return invalidArgument(ErrInvalidOperation, "invalid operation name", "invalid_operation", nil)
	}
	if len(value) == 0 {
		*operation = Operation{}
		return nil
	}
	parsed, err := ParseOperation(string(value))
	if err != nil {
		return err
	}
	*operation = parsed
	return nil
}

func (operation Operation) MarshalJSON() ([]byte, error) {
	if operation.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(operation.value)
}

func (operation *Operation) UnmarshalJSON(value []byte) error {
	if operation == nil {
		return invalidArgument(ErrInvalidOperation, "invalid operation name", "invalid_operation", nil)
	}
	if bytes.Equal(value, []byte("null")) {
		*operation = Operation{}
		return nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return invalidArgument(errors.Join(ErrInvalidOperation, err), "invalid operation name", "invalid_operation", nil)
	}
	return operation.UnmarshalText([]byte(text))
}

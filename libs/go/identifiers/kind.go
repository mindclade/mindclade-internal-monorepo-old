// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package identifiers

const (
	// MinimumKindLength and MaximumKindLength bound identifier prefixes.
	MinimumKindLength = 2
	MaximumKindLength = 24
)

// Kind is the canonical lowercase prefix of a resource ID, such as "run",
// "org", or "model". Kinds contain only ASCII lowercase letters and digits,
// begin with a letter, and do not contain the underscore separator.
type Kind string

// ParseKind validates value as a canonical Kind.
func ParseKind(value string) (Kind, error) {
	kind := Kind(value)
	if err := kind.Validate(); err != nil {
		return "", err
	}
	return kind, nil
}

// MustParseKind is ParseKind for package-level constants and test fixtures. It
// panics when value is invalid and must not be used on untrusted input.
func MustParseKind(value string) Kind {
	kind, err := ParseKind(value)
	if err != nil {
		panic(err)
	}
	return kind
}

// String returns the canonical prefix.
func (kind Kind) String() string {
	return string(kind)
}

// Valid reports whether kind is canonical.
func (kind Kind) Valid() bool {
	return kind.Validate() == nil
}

// Validate checks the canonical Kind grammar.
func (kind Kind) Validate() error {
	value := string(kind)
	if len(value) < MinimumKindLength || len(value) > MaximumKindLength {
		return invalidValue(
			"kind",
			value,
			"length must be between 2 and 24 bytes",
			ErrInvalidKind,
		)
	}

	for index := 0; index < len(value); index++ {
		character := value[index]
		isLetter := character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		if index == 0 && !isLetter {
			return invalidValue(
				"kind",
				value,
				"first character must be an ASCII lowercase letter",
				ErrInvalidKind,
			)
		}
		if !isLetter && !isDigit {
			return invalidValue(
				"kind",
				value,
				"characters must be ASCII lowercase letters or digits",
				ErrInvalidKind,
			)
		}
	}
	return nil
}

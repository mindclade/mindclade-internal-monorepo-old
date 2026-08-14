// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package identifiers

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	DigestAlgorithm  = "sha256"
	DigestBinarySize = sha256.Size
	DigestHexLength  = sha256.Size * 2
	DigestTextLength = len(DigestAlgorithm) + 1 + DigestHexLength
)

// Digest is an optional SHA-256 content digest. Its canonical text form is
// "sha256:<64 lowercase hexadecimal characters>".
//
// The unexported presence bit lets the zero value represent absence without
// making an all-zero 32-byte digest unrepresentable.
type Digest struct {
	sum     [DigestBinarySize]byte
	present bool
}

// SHA256 returns the digest of value.
func SHA256(value []byte) Digest {
	return Digest{
		sum:     sha256.Sum256(value),
		present: true,
	}
}

// SHA256String returns the digest of value's UTF-8 bytes.
func SHA256String(value string) Digest {
	return SHA256([]byte(value))
}

// SHA256Reader streams reader into a SHA-256 hash and returns the digest and
// number of bytes consumed.
func SHA256Reader(reader io.Reader) (Digest, int64, error) {
	if reader == nil {
		return Digest{}, 0, invalidValue(
			"digest reader",
			"",
			"must not be nil",
			ErrInvalidDigest,
		)
	}

	hash := sha256.New()
	count, err := io.Copy(hash, reader)
	if err != nil {
		return Digest{}, count, fmt.Errorf("identifiers: compute sha256 digest: %w", err)
	}
	digest, err := DigestFromBytes(hash.Sum(nil))
	if err != nil {
		return Digest{}, count, err
	}
	return digest, count, nil
}

// DigestFromBytes constructs a present digest from exactly 32 raw SHA-256
// bytes, including an all-zero byte sequence.
func DigestFromBytes(value []byte) (Digest, error) {
	if len(value) != DigestBinarySize {
		return Digest{}, invalidValue(
			"digest bytes",
			fmt.Sprintf("length=%d", len(value)),
			"expected exactly 32 bytes",
			ErrInvalidDigest,
		)
	}
	var digest Digest
	copy(digest.sum[:], value)
	digest.present = true
	return digest, nil
}

// ParseDigest parses canonical digest text. It rejects uppercase and alternate
// algorithm spellings to preserve one representation for signatures and keys.
func ParseDigest(value string) (Digest, error) {
	if len(value) != DigestTextLength {
		return Digest{}, invalidValue(
			"digest",
			value,
			"unexpected length",
			ErrInvalidDigest,
		)
	}
	if !strings.HasPrefix(value, DigestAlgorithm+":") {
		return Digest{}, invalidValue(
			"digest",
			value,
			"expected sha256 algorithm prefix",
			ErrInvalidDigest,
		)
	}

	hexValue := value[len(DigestAlgorithm)+1:]
	if hexValue != strings.ToLower(hexValue) {
		return Digest{}, invalidValue(
			"digest",
			value,
			"hexadecimal value must be lowercase",
			ErrInvalidDigest,
		)
	}
	decoded, err := hex.DecodeString(hexValue)
	if err != nil {
		return Digest{}, invalidValue(
			"digest",
			value,
			"contains non-hexadecimal characters",
			ErrInvalidDigest,
		)
	}
	return DigestFromBytes(decoded)
}

// MustParseDigest is ParseDigest for constants and fixtures. It panics on
// invalid input.
func MustParseDigest(value string) Digest {
	digest, err := ParseDigest(value)
	if err != nil {
		panic(err)
	}
	return digest
}

// String returns canonical digest text, or an empty string for an absent
// digest.
func (digest Digest) String() string {
	if !digest.present {
		return ""
	}
	return DigestAlgorithm + ":" + digest.Hex()
}

// Hex returns the lowercase 64-character digest value without an algorithm
// prefix, or an empty string for an absent digest.
func (digest Digest) Hex() string {
	if !digest.present {
		return ""
	}
	var output [DigestHexLength]byte
	hex.Encode(output[:], digest.sum[:])
	return string(output[:])
}

// Bytes returns an independent copy of the raw digest, or nil when absent.
func (digest Digest) Bytes() []byte {
	if !digest.present {
		return nil
	}
	output := make([]byte, DigestBinarySize)
	copy(output, digest.sum[:])
	return output
}

// IsZero reports whether digest is absent.
func (digest Digest) IsZero() bool {
	return !digest.present
}

// Valid reports whether digest is present.
func (digest Digest) Valid() bool {
	return digest.present
}

// Equal compares digest presence and bytes in constant time.
func (digest Digest) Equal(other Digest) bool {
	if digest.present != other.present {
		return false
	}
	if !digest.present {
		return true
	}
	return subtle.ConstantTimeCompare(digest.sum[:], other.sum[:]) == 1
}

// Verify reports whether value has this digest. An absent digest never
// verifies successfully.
func (digest Digest) Verify(value []byte) bool {
	return digest.present && digest.Equal(SHA256(value))
}

func (digest Digest) MarshalText() ([]byte, error) {
	if !digest.present {
		return []byte{}, nil
	}
	return []byte(digest.String()), nil
}

func (digest *Digest) UnmarshalText(value []byte) error {
	if digest == nil {
		return invalidValue("digest", string(value), "nil destination", ErrInvalidDigest)
	}
	if len(value) == 0 {
		*digest = Digest{}
		return nil
	}
	parsed, err := ParseDigest(string(value))
	if err != nil {
		return err
	}
	*digest = parsed
	return nil
}

func (digest Digest) MarshalJSON() ([]byte, error) {
	if !digest.present {
		return []byte("null"), nil
	}
	return json.Marshal(digest.String())
}

func (digest *Digest) UnmarshalJSON(value []byte) error {
	if digest == nil {
		return invalidValue("digest", string(value), "nil destination", ErrInvalidDigest)
	}
	if bytes.Equal(value, []byte("null")) {
		*digest = Digest{}
		return nil
	}

	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return invalidValue("digest", string(value), "expected a JSON string or null", ErrInvalidDigest)
	}
	return digest.UnmarshalText([]byte(text))
}

// Value implements driver.Valuer. An absent digest becomes SQL NULL.
func (digest Digest) Value() (driver.Value, error) {
	if !digest.present {
		return nil, nil
	}
	return digest.String(), nil
}

// Scan implements sql.Scanner for canonical text or raw 32-byte values.
func (digest *Digest) Scan(value any) error {
	if digest == nil {
		return invalidValue("digest", "", "nil destination", ErrInvalidDigest)
	}

	switch typed := value.(type) {
	case nil:
		*digest = Digest{}
		return nil
	case string:
		return digest.UnmarshalText([]byte(typed))
	case []byte:
		if len(typed) == DigestBinarySize {
			parsed, err := DigestFromBytes(typed)
			if err != nil {
				return err
			}
			*digest = parsed
			return nil
		}
		return digest.UnmarshalText(typed)
	default:
		return invalidValue(
			"digest",
			fmt.Sprintf("%T", value),
			"unsupported SQL source type",
			ErrInvalidDigest,
		)
	}
}

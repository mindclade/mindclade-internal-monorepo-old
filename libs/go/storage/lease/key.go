// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package lease

import (
	"strings"
	"unicode"

	"mindclade.internal/libs/go/faults"
)

const MaximumKeyLength = 256

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
func (key Key) Validate() error {
	value := string(key)
	if value == "" || len(value) > MaximumKeyLength || strings.TrimSpace(value) != value {
		return faults.Wrap(ErrInvalidKey, faults.CodeInvalidArgument, "invalid lease key", faults.WithReason("invalid_lease_key"), faults.WithOperation("lease.Key.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return faults.Wrap(ErrInvalidKey, faults.CodeInvalidArgument, "invalid lease key", faults.WithReason("invalid_lease_key_character"), faults.WithOperation("lease.Key.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
		}
	}
	return nil
}

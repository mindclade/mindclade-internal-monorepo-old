// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package cursor

import (
	"strings"

	"mindclade.internal/libs/go/faults"
)

const MaximumKeyPartBytes = 128

type Key struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func NewKey(namespace, name string) (Key, error) {
	key := Key{Namespace: strings.TrimSpace(namespace), Name: strings.TrimSpace(name)}
	if err := key.Validate(); err != nil {
		return Key{}, err
	}
	return key, nil
}
func (key Key) String() string {
	if key.Namespace == "" || key.Name == "" {
		return ""
	}
	return key.Namespace + "/" + key.Name
}
func (key Key) Validate() error {
	if !canonical(key.Namespace) || !canonical(key.Name) {
		return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "invalid cursor key", faults.WithReason("invalid_cursor_key"), faults.WithOperation("cursor.Key.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}
func canonical(value string) bool {
	if value == "" || len(value) > MaximumKeyPartBytes || value != strings.TrimSpace(value) {
		return false
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		if i > 0 && (r == '-' || r == '_' || r == '.' || r == ':') {
			continue
		}
		return false
	}
	return true
}

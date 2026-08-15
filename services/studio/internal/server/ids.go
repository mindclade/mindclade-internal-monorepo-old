// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// newUUID returns a RFC 4122 version 4 UUID.
//
// Generated here rather than by the database so that an idempotent submission
// can propose an id and let the unique constraint decide whether it is used —
// `INSERT … ON CONFLICT DO NOTHING RETURNING id` needs the id up front.
func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing means the system has no entropy source. There is
		// nothing sensible to return, and returning a predictable id would be
		// worse than stopping.
		panic(fmt.Sprintf("server: crypto/rand unavailable: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// digest hashes a request body for idempotency comparison.
//
// The digest — not the body — is stored, so replaying a key with a different
// body is detectable without keeping every submitted payload alongside every
// run forever.
func digest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

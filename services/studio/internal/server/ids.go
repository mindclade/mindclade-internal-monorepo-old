// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package server

import (
	"fmt"

	"go.mindclade.dev/libs/go/identifiers"
)

// newUUID returns a RFC 4122 version 4 UUID.
//
// Generated here rather than by the database so that an idempotent submission
// can propose an id and let the unique constraint decide whether it is used —
// `INSERT … ON CONFLICT DO NOTHING RETURNING id` needs the id up front.
//
// The version and variant bit-setting lives in libs/go/identifiers, which is
// conformance-tested against the RFC. Studio holds the panic policy rather than
// the encoding: identifiers reports an entropy failure as an error, and the
// choice to treat it as unrecoverable is this service's.
func newUUID() string {
	uuid, err := identifiers.NewUUIDv4()
	if err != nil {
		// crypto/rand failing means the system has no entropy source. There is
		// nothing sensible to return, and returning a predictable id would be
		// worse than stopping.
		panic(fmt.Sprintf("server: crypto/rand unavailable: %v", err))
	}
	return uuid.String()
}

// digest hashes a request body for idempotency comparison.
//
// The digest — not the body — is stored, so replaying a key with a different
// body is detectable without keeping every submitted payload alongside every
// run forever.
//
// Hex rather than identifiers.Digest.String(): the stored form predates this
// and carries no algorithm prefix, so emitting one would not match rows already
// written.
func digest(body []byte) string {
	return identifiers.SHA256(body).Hex()
}

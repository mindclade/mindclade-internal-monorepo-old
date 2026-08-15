// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package signing

import "strings"

type Algorithm string

const (
	AlgorithmHMACSHA256 Algorithm = "hmac-sha256"
	AlgorithmEd25519    Algorithm = "ed25519"
)

func (algorithm Algorithm) String() string { return string(algorithm) }
func (algorithm Algorithm) Valid() bool {
	return algorithm == AlgorithmHMACSHA256 || algorithm == AlgorithmEd25519
}
func ParseAlgorithm(value string) (Algorithm, error) {
	algorithm := Algorithm(strings.ToLower(strings.TrimSpace(value)))
	if !algorithm.Valid() {
		return "", invalid(ErrInvalidAlgorithm, "invalid signing algorithm", "invalid_signing_algorithm", "signing.ParseAlgorithm", map[string]any{"algorithm": value})
	}
	return algorithm, nil
}

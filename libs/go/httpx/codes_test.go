// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpx

import (
	"net/http"
	"testing"

	"mindclade.internal/libs/go/faults"
)

func TestCodeStatusMappings(t *testing.T) {
	cases := []struct {
		code   faults.Code
		status int
	}{
		{faults.CodeInvalidArgument, http.StatusBadRequest},
		{faults.CodeNotFound, http.StatusNotFound},
		{faults.CodeUnauthenticated, http.StatusUnauthorized},
		{faults.CodePermissionDenied, http.StatusForbidden},
		{faults.CodeResourceExhausted, http.StatusTooManyRequests},
		{faults.CodeUnavailable, http.StatusServiceUnavailable},
		{faults.CodeInternal, http.StatusInternalServerError},
	}
	for _, test := range cases {
		if got := StatusFromCode(test.code); got != test.status {
			t.Errorf("%s => %d", test.code, got)
		}
	}
}

// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpxtest

import (
	"encoding/json"
	"net/http"
	"testing"

	"go.mindclade.dev/libs/go/httpx"
)

func DecodeProblem(testingTB testing.TB, response *http.Response) httpx.Problem {
	testingTB.Helper()
	defer response.Body.Close()
	var problem httpx.Problem
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		testingTB.Fatalf("decode problem: %v", err)
	}
	return problem
}

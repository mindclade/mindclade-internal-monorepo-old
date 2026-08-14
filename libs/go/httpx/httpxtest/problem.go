// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package httpxtest

import (
	"encoding/json"
	"net/http"
	"testing"

	"mindclade.internal/libs/go/httpx"
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

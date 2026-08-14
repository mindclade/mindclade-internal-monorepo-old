// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package httpxtest

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDecodeProblem(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader(`{"type":"about:blank","title":"Not Found","status":404,"code":"not_found","detail":"resource not found"}`)),
	}
	problem := DecodeProblem(t, response)
	if problem.Status != http.StatusNotFound || problem.Code != "not_found" {
		t.Fatalf("problem = %#v", problem)
	}
}

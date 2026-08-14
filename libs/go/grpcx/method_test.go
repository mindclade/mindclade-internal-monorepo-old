// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package grpcx

import "testing"

func TestParseMethod(t *testing.T) {
	method, err := ParseMethod("/mindclade.runs.v1.RunService/Create")
	if err != nil {
		t.Fatal(err)
	}
	if method.Service != "mindclade.runs.v1.RunService" || method.Name != "Create" {
		t.Fatalf("%+v", method)
	}
}

func TestParseMethodRejectsInvalidIdentifiers(t *testing.T) {
	invalid := []string{
		"/mindclade..v1.Service/Get",
		"/mindclade.v1.Σervice/Get",
		"/mindclade.v1.Service/1Get",
		"/mindclade.v1.Service/Get.More",
		"mindclade.v1.Service/Get",
	}
	for _, value := range invalid {
		if _, err := ParseMethod(value); err == nil {
			t.Fatalf("expected invalid method %q", value)
		}
	}
}

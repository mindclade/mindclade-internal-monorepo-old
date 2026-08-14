// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package transport

import (
	"errors"
	"testing"

	"mindclade.internal/libs/go/faults"
)

func TestBundleRejectsConnectWithoutHTTP(t *testing.T) {
	_, err := (Bundle{Connect: true}).Components()
	if err == nil || faults.ReasonOf(err) != "connect_without_http" {
		t.Fatalf("err=%v", err)
	}
}

func TestBundleRejectsEmpty(t *testing.T) {
	_, err := (Bundle{}).Components()
	if err == nil || faults.ReasonOf(err) != "empty_transport_bundle" {
		t.Fatalf("err=%v", err)
	}
	if errors.Is(err, nil) {
		t.Fatal("expected structured error")
	}
}

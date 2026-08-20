// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package transport

import (
	"errors"
	"testing"

	"go.mindclade.dev/libs/go/faults"
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

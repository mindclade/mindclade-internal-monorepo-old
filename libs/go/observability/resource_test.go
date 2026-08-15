// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package observability

import (
	"errors"
	"testing"

	"mindclade.internal/libs/go/faults"
)

func TestResourceCanonicalAttributesTakePrecedence(t *testing.T) {
	resource, err := NewResource(
		"control-plane",
		WithServiceNamespace("mindclade"),
		WithServiceVersion("1.2.3"),
		WithResourceAttributes(MustAttributes(faults.Fields{
			"service.name": "attacker",
			"custom.role":  "api",
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	fields := resource.Attributes().Fields()
	if fields["service.name"] != "control-plane" {
		t.Fatalf("service.name = %v, want canonical value", fields["service.name"])
	}
	if fields["service.namespace"] != "mindclade" || fields["custom.role"] != "api" {
		t.Fatalf("resource attributes = %#v", fields)
	}
}

func TestResourceValidation(t *testing.T) {
	if _, err := NewResource(""); !errors.Is(err, ErrInvalidResource) {
		t.Fatalf("NewResource(empty) error = %v", err)
	}
	if _, err := NewResource("control plane"); !errors.Is(err, ErrInvalidResource) {
		t.Fatalf("NewResource(space) error = %v", err)
	}
	if _, err := NewResource("service", WithCloudRegion("bad region")); !errors.Is(err, ErrInvalidResource) {
		t.Fatalf("NewResource(invalid option) error = %v", err)
	}
}

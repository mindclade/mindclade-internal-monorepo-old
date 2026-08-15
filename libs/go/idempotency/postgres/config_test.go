// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import "testing"

func TestValidQualifiedIdentifier(t *testing.T) {
	t.Parallel()
	valid := []string{"mindclade_idempotency_records", "mindclade.idempotency_records", "a1.b2"}
	for _, value := range valid {
		if !validQualifiedIdentifier(value) {
			t.Fatalf("expected %q to be valid", value)
		}
	}
	invalid := []string{"", "Public.records", "public..records", "public.records;drop", "1records", ".records", "records."}
	for _, value := range invalid {
		if validQualifiedIdentifier(value) {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}

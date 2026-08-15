// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package idempotency

import (
	"mindclade.internal/libs/go/identifiers"
	"testing"
	"time"
)

func TestRecordStateValidation(t *testing.T) {
	now := time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC)
	id, err := identifiers.NewIDAt(RecordIDKind, now)
	if err != nil {
		t.Fatal(err)
	}
	identity, _ := NewIdentity(MustParseScope("control/runs.create"), MustParseKey("request-123456"))
	data := RecordData{ID: id, Identity: identity, Fingerprint: identifiers.SHA256String("request"), State: StateInProgress, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour), LeaseExpiresAt: now.Add(time.Minute), Version: 1}
	record, err := NewRecord(data)
	if err != nil {
		t.Fatal(err)
	}
	if record.LeaseExpired(now) {
		t.Fatal("lease expired early")
	}
	result, _ := NewResult([]byte("done"), "text/plain", nil)
	data = record.Data()
	data.State = StateCompleted
	data.Result = result
	data.LeaseExpiresAt = time.Time{}
	data.Version++
	completed, err := NewRecord(data)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State() != StateCompleted {
		t.Fatal("not completed")
	}
}

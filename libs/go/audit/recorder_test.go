// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package audit

import (
	"context"
	"errors"
	"testing"

	"mindclade.internal/libs/go/faults"
)

func TestRecord(t *testing.T) {
	factory, actor, target, _ := auditFixture(t)
	event, err := factory.Create(MustParseAction("runs.read"), actor, target, OutcomeSucceeded)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	recorder := RecorderFunc(func(context.Context, Event) error { called = true; return nil })
	if err := Record(context.Background(), recorder, event); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("recorder was not called")
	}
}

func TestRecordClassifiesFailure(t *testing.T) {
	factory, actor, target, _ := auditFixture(t)
	event, err := factory.Create(MustParseAction("runs.read"), actor, target, OutcomeSucceeded)
	if err != nil {
		t.Fatal(err)
	}
	recorder := RecorderFunc(func(context.Context, Event) error { return errors.New("queue offline") })
	err = Record(context.Background(), recorder, event)
	if faults.CodeOf(err) != faults.CodeUnavailable || !faults.IsRetryable(err) {
		t.Fatalf("Record() error = %v", err)
	}
}

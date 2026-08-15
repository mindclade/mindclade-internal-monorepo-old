// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package leasetest

import (
	"context"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/storage/lease"
)

type Factory func(testing.TB) lease.Store

func Run(t *testing.T, factory Factory) {
	t.Helper()
	store := factory(t)
	ctx := context.Background()
	request := lease.AcquireRequest{Key: lease.MustParseKey("conformance/lease"), Owner: "owner-a", TTL: time.Minute}
	first, err := store.Acquire(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(ctx, request); !faults.IsCode(err, faults.CodeConflict) {
		t.Fatalf("second acquire = %v", err)
	}
	current, err := store.Lookup(ctx, request.Key)
	if err != nil || !current.Token.Equal(first.Token) {
		t.Fatalf("Lookup = %#v, %v", current, err)
	}
	renewed, err := store.Renew(ctx, first, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Version <= first.Version {
		t.Fatal("version did not advance")
	}
	if err := store.Release(ctx, first); !faults.IsCode(err, faults.CodeConflict) {
		t.Fatalf("stale release = %v", err)
	}
	if err := store.Release(ctx, renewed); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Lookup(ctx, request.Key); !faults.IsCode(err, faults.CodeNotFound) {
		t.Fatalf("lookup after release = %v", err)
	}
}

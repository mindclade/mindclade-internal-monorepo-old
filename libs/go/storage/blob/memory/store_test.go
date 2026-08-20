// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package memory

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/storage/blob"
)

func TestStoreLifecycle(t *testing.T) {
	fake := clock.NewFake(time.Unix(100, 0))
	store, err := New(WithClock(fake))
	if err != nil {
		t.Fatal(err)
	}
	key := blob.MustParseKey("runs/1/result.cif")
	attributes, err := store.Put(context.Background(), key, bytes.NewBufferString("abc"), blob.PutOptions{ContentType: "chemical/x-mmcif", Preconditions: blob.Preconditions{IfNotExists: true}})
	if err != nil {
		t.Fatal(err)
	}
	object, err := store.Open(context.Background(), key, blob.GetOptions{Offset: 1, Length: 1})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(object.Body)
	_ = object.Close()
	if string(data) != "b" {
		t.Fatalf("range data = %q", data)
	}
	generation := attributes.Generation
	if _, err := store.Put(context.Background(), key, bytes.NewBufferString("new"), blob.PutOptions{Preconditions: blob.Preconditions{IfGenerationMatch: &generation}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), key, bytes.NewBufferString("bad"), blob.PutOptions{Preconditions: blob.Preconditions{IfGenerationMatch: &generation}}); !faults.IsCode(err, faults.CodeConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	page, err := store.List(context.Background(), blob.ListOptions{Prefix: "runs/"})
	if err != nil || len(page.Objects) != 1 {
		t.Fatalf("list = %#v, %v", page, err)
	}
}

// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package blobtest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/storage/blob"
)

// Factory returns an isolated store for one conformance subtest.
type Factory func(testing.TB) blob.Store

// Run executes the provider-neutral blob contract. Provider packages should
// invoke it from their integration test suite.
func Run(t *testing.T, factory Factory) {
	t.Helper()
	if factory == nil {
		t.Fatal("blobtest: nil factory")
	}

	t.Run("round_trip", func(t *testing.T) {
		store := requireStore(t, factory)
		key := blob.MustParseKey("conformance/round-trip.bin")
		payload := []byte("mindclade blob conformance")
		attributes, err := store.Put(context.Background(), key, bytes.NewReader(payload), blob.PutOptions{
			ContentType: "application/octet-stream",
			Metadata:    blob.Metadata{"suite": "blobtest"},
			Digest:      identifiers.SHA256(payload),
			Preconditions: blob.Preconditions{
				IfNotExists: true,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if attributes.Key != key || attributes.Size != int64(len(payload)) || !attributes.Digest.Equal(identifiers.SHA256(payload)) {
			t.Fatalf("unexpected attributes: %#v", attributes)
		}

		object, err := store.Open(context.Background(), key, blob.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		got, readErr := io.ReadAll(object.Body)
		closeErr := object.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read=%v close=%v", readErr, closeErr)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("payload = %q", got)
		}
		if object.Attributes.Generation != attributes.Generation {
			t.Fatalf("object mismatch: %#v", object.Attributes)
		}
	})

	t.Run("preconditions", func(t *testing.T) {
		store := requireStore(t, factory)
		key := blob.MustParseKey("conformance/conditions")
		first, err := store.Put(context.Background(), key, bytes.NewBufferString("first"), blob.PutOptions{Preconditions: blob.Preconditions{IfNotExists: true}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.Put(context.Background(), key, bytes.NewBufferString("again"), blob.PutOptions{Preconditions: blob.Preconditions{IfNotExists: true}})
		if err == nil || !errors.Is(err, blob.ErrPrecondition) {
			t.Fatalf("create-only error = %v", err)
		}
		stale := first.Generation + 100
		_, err = store.Put(context.Background(), key, bytes.NewBufferString("second"), blob.PutOptions{Preconditions: blob.Preconditions{IfGenerationMatch: &stale}})
		if err == nil || !errors.Is(err, blob.ErrPrecondition) {
			t.Fatalf("stale generation error = %v", err)
		}
		match := first.Generation
		second, err := store.Put(context.Background(), key, bytes.NewBufferString("second"), blob.PutOptions{Preconditions: blob.Preconditions{IfGenerationMatch: &match}})
		if err != nil {
			t.Fatal(err)
		}
		if second.Generation == first.Generation {
			t.Fatal("generation did not advance")
		}
	})

	t.Run("list_and_delete", func(t *testing.T) {
		store := requireStore(t, factory)
		for _, value := range []string{"conformance/list/1", "conformance/list/2", "conformance/list/3", "other/1"} {
			key := blob.MustParseKey(value)
			if _, err := store.Put(context.Background(), key, bytes.NewBufferString(value), blob.PutOptions{}); err != nil {
				t.Fatal(err)
			}
		}
		first, err := store.List(context.Background(), blob.ListOptions{Prefix: "conformance/list/", Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Objects) != 2 || first.NextCursor == "" {
			t.Fatalf("first page = %#v", first)
		}
		second, err := store.List(context.Background(), blob.ListOptions{Prefix: "conformance/list/", Limit: 2, Cursor: first.NextCursor})
		if err != nil {
			t.Fatal(err)
		}
		if len(second.Objects) != 1 {
			t.Fatalf("second page = %#v", second)
		}
		key := first.Objects[0].Key
		generation := first.Objects[0].Generation
		if err := store.Delete(context.Background(), key, blob.DeleteOptions{Preconditions: blob.Preconditions{IfGenerationMatch: &generation}}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Stat(context.Background(), key); err == nil || faults.CodeOf(err) != faults.CodeNotFound {
			t.Fatalf("Stat after delete = %v", err)
		}
	})

	t.Run("range_and_digest", func(t *testing.T) {
		store := requireStore(t, factory)
		key := blob.MustParseKey("conformance/range")
		payload := []byte("0123456789")
		if _, err := store.Put(context.Background(), key, bytes.NewReader(payload), blob.PutOptions{}); err != nil {
			t.Fatal(err)
		}
		object, err := store.Open(context.Background(), key, blob.GetOptions{Offset: 2, Length: 5})
		if err != nil {
			t.Fatal(err)
		}
		got, readErr := io.ReadAll(object.Body)
		_ = object.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != "23456" {
			t.Fatalf("range = %q", got)
		}
		_, err = store.Put(context.Background(), blob.MustParseKey("conformance/bad-digest"), bytes.NewReader(payload), blob.PutOptions{Digest: identifiers.SHA256String("wrong")})
		if err == nil || !errors.Is(err, blob.ErrDigestMismatch) || faults.CodeOf(err) != faults.CodeDataLoss {
			t.Fatalf("digest error = %v", err)
		}
	})
}

func requireStore(t testing.TB, factory Factory) blob.Store {
	t.Helper()
	store := factory(t)
	if store == nil {
		t.Fatal("blobtest: factory returned nil store")
	}
	return store
}

// PutString stores value and fails test on error.
func PutString(t testing.TB, store blob.Store, key blob.Key, value string) blob.Attributes {
	t.Helper()
	if store == nil {
		t.Fatal("blobtest: nil store")
	}
	attributes, err := store.Put(context.Background(), key, bytes.NewBufferString(value), blob.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return attributes
}

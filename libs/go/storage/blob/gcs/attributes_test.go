// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package gcs

import (
	"testing"
	"time"

	gcsapi "cloud.google.com/go/storage"

	"mindclade.internal/libs/go/identifiers"
)

func TestConvertAttributes(t *testing.T) {
	store := &Store{prefix: "tenant/"}
	digest := identifiers.SHA256String("payload")
	created := time.Unix(100, 0).UTC()
	attributes, err := store.convertAttributes(&gcsapi.ObjectAttrs{
		Name:        "tenant/path/object",
		Size:        7,
		ContentType: "application/octet-stream",
		Etag:        "etag",
		Generation:  2,
		Created:     created,
		Updated:     created.Add(time.Second),
		Metadata:    map[string]string{DigestMetadataKey: digest.String(), "schema": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if attributes.Key.String() != "path/object" || !attributes.Digest.Equal(digest) || attributes.Metadata["schema"] != "1" {
		t.Fatalf("attributes = %#v", attributes)
	}
	if _, reserved := attributes.Metadata[DigestMetadataKey]; reserved {
		t.Fatal("reserved digest metadata leaked")
	}
}

func TestConvertAttributesRejectsInvalidDigest(t *testing.T) {
	store := &Store{}
	_, err := store.convertAttributes(&gcsapi.ObjectAttrs{Name: "object", Generation: 1, Metadata: map[string]string{DigestMetadataKey: "invalid"}})
	if err == nil {
		t.Fatal("convertAttributes() returned nil")
	}
}

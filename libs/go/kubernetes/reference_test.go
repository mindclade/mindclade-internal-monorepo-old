// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package kubernetes

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func TestReferenceFor(t *testing.T) {
	object := &metav1.PartialObjectMetadata{}
	object.SetNamespace("default")
	object.SetName("run-1")
	object.SetUID(types.UID("uid-1"))
	reference := ReferenceFor(object, schema.GroupVersionKind{Group: "mindclade.dev", Version: "v1", Kind: "Run"})
	if reference.APIVersion != "mindclade.dev/v1" || reference.Kind != "Run" || reference.Name != "run-1" {
		t.Fatalf("reference = %#v", reference)
	}
	if fields := reference.Fields(); fields[FieldUID] != "uid-1" {
		t.Fatalf("fields = %#v", fields)
	}
}

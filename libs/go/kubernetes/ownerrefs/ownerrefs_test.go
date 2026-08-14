// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package ownerrefs

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestRejectsCrossNamespaceReference(t *testing.T) {
	owner := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "one"}}
	object := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "object", Namespace: "two"}}
	if _, err := SetOwner(owner, object, runtime.NewScheme()); err == nil {
		t.Fatal("SetOwner() returned nil")
	}
}

func TestRejectsNamespacedOwnerForClusterScopedObject(t *testing.T) {
	owner := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "one"}}
	object := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "cluster-object"}}
	if _, err := SetOwner(owner, object, runtime.NewScheme()); err == nil {
		t.Fatal("SetOwner() returned nil")
	}
}

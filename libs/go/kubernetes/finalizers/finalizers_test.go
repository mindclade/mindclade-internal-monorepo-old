// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package finalizers

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestAddRemove(t *testing.T) {
	object := &metav1.PartialObjectMetadata{}
	name := MustParse("mindclade.dev/cleanup")
	changed, err := Add(object, name)
	if err != nil || !changed || !Contains(object, name) {
		t.Fatalf("Add() = (%v, %v), finalizers=%#v", changed, err, object.GetFinalizers())
	}
	changed, err = Add(object, name)
	if err != nil || changed {
		t.Fatalf("idempotent Add() = (%v, %v)", changed, err)
	}
	changed, err = Remove(object, name)
	if err != nil || !changed || Contains(object, name) {
		t.Fatalf("Remove() = (%v, %v), finalizers=%#v", changed, err, object.GetFinalizers())
	}
}

func TestParseRequiresQualifiedName(t *testing.T) {
	for _, value := range []string{"", "cleanup", "bad name/finalizer"} {
		if _, err := Parse(value); err == nil {
			t.Fatalf("Parse(%q) returned nil", value)
		}
	}
}

type failingPatchClient struct {
	crclient.Client
	err error
}

func (client *failingPatchClient) Patch(context.Context, crclient.Object, crclient.Patch, ...crclient.PatchOption) error {
	return client.err
}

func TestEnsureRestoresObjectWhenPatchFails(t *testing.T) {
	object := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{"mindclade.dev/existing"}}}
	client := &failingPatchClient{err: errors.New("conflict")}
	changed, err := Ensure(context.Background(), client, object, MustParse("mindclade.dev/cleanup"))
	if err == nil || changed {
		t.Fatalf("Ensure() = (%v, %v)", changed, err)
	}
	if got := object.GetFinalizers(); len(got) != 1 || got[0] != "mindclade.dev/existing" {
		t.Fatalf("finalizers after failed patch = %#v", got)
	}
}

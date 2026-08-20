// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package kubernetes_foundation exercises the reconciler-side halves of
// libs/go/kubernetes from outside their own packages.
//
// These four packages are stateless helpers a reconcile loop calls; no
// composition root can hold them, so unlike the client, the manager, and the
// recorder they gain no importer when a provider factory is materialized.
// Their in-package tests cannot supply one either -- a test in `package
// status` is the package, not a caller of it.
//
// This suite is that caller. It drives the helpers in the order a reconcile
// pass actually uses them: adopt the object, take the finalizer, publish the
// outcome as a condition, then release the finalizer on deletion.
package kubernetes_foundation

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8swatch "k8s.io/apimachinery/pkg/watch"

	"go.mindclade.dev/libs/go/kubernetes/finalizers"
	"go.mindclade.dev/libs/go/kubernetes/ownerrefs"
	"go.mindclade.dev/libs/go/kubernetes/status"
	"go.mindclade.dev/libs/go/kubernetes/watch"
)

const cleanupFinalizer = "mindclade.dev/stage-cleanup"

// owning is the parent kind, and the metadata carrier both kinds share. The
// two are distinct Go types because a scheme maps one type to one
// GroupVersionKind; reusing a single type for both ends leaves ownerrefs
// unable to name the owner's kind. Only DeepCopyObject is restated, and only
// because it must return the concrete type.
type owning struct {
	metav1.PartialObjectMetadata
}

func (object *owning) DeepCopyObject() runtime.Object {
	return &owning{PartialObjectMetadata: *object.DeepCopy()}
}

// conditioned is the reconciled kind: the same metadata plus the condition
// list the status helpers read and write.
type conditioned struct {
	owning
	conditions []metav1.Condition
}

func (object *conditioned) GetConditions() []metav1.Condition { return object.conditions }
func (object *conditioned) SetConditions(values []metav1.Condition) {
	object.conditions = append([]metav1.Condition(nil), values...)
}

func (object *conditioned) DeepCopyObject() runtime.Object {
	copied := &conditioned{owning: owning{PartialObjectMetadata: *object.DeepCopy()}}
	copied.conditions = append([]metav1.Condition(nil), object.conditions...)
	return copied
}

// namespacedScheme registers the owner and the controlled object as
// namespace-scoped members of one group, which is what ownerrefs requires
// before it will resolve a GroupVersionKind.
func namespacedScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	group := schema.GroupVersion{Group: "mindclade.dev", Version: "v1"}
	value := runtime.NewScheme()
	value.AddKnownTypeWithName(group.WithKind("Stage"), &conditioned{})
	value.AddKnownTypeWithName(group.WithKind("Run"), &owning{})
	metav1.AddToGroupVersion(value, group)
	return value
}

func objectMeta(name, namespace string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: namespace, UID: types.UID("uid-" + name)}
}

func stage(name, namespace string) *conditioned {
	return &conditioned{owning: *run(name, namespace)}
}

func run(name, namespace string) *owning {
	return &owning{PartialObjectMetadata: metav1.PartialObjectMetadata{ObjectMeta: objectMeta(name, namespace)}}
}

// A reconcile pass adopts the object, takes a finalizer, and records the
// outcome. Running it twice must change nothing the second time: a reconciler
// is called repeatedly for the same generation and may not accumulate owner
// references, finalizers, or duplicate conditions.
func TestReconcilePassIsIdempotent(t *testing.T) {
	value := namespacedScheme(t)
	owner, object := run("run", "tenant"), stage("stage", "tenant")
	name := finalizers.MustParse(cleanupFinalizer)
	observed := time.Unix(1_800_000_000, 0).UTC()
	condition := metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled"}

	for pass := 1; pass <= 2; pass++ {
		first := pass == 1

		adopted, err := ownerrefs.SetController(owner, object, value)
		if err != nil {
			t.Fatalf("pass %d: SetController() = %v", pass, err)
		}
		if adopted != first {
			t.Fatalf("pass %d: SetController() changed=%v, want %v", pass, adopted, first)
		}

		taken, err := finalizers.Add(object, name)
		if err != nil {
			t.Fatalf("pass %d: Add() = %v", pass, err)
		}
		if taken != first {
			t.Fatalf("pass %d: Add() changed=%v, want %v", pass, taken, first)
		}

		published, err := status.SetCondition(object, condition, observed)
		if err != nil {
			t.Fatalf("pass %d: SetCondition() = %v", pass, err)
		}
		if published != first {
			t.Fatalf("pass %d: SetCondition() changed=%v, want %v", pass, published, first)
		}
	}

	if references := object.GetOwnerReferences(); len(references) != 1 {
		t.Fatalf("owner references=%#v", references)
	}
	if values := object.GetFinalizers(); len(values) != 1 || values[0] != cleanupFinalizer {
		t.Fatalf("finalizers=%#v", values)
	}
	if len(object.conditions) != 1 || object.conditions[0].Status != metav1.ConditionTrue {
		t.Fatalf("conditions=%#v", object.conditions)
	}
}

// Deletion is the other half of the pass: the reconciler marks the object not
// ready and releases its finalizer so the API server can collect it.
func TestDeletionReleasesTheFinalizer(t *testing.T) {
	object := stage("stage", "tenant")
	name := finalizers.MustParse(cleanupFinalizer)
	if _, err := finalizers.Add(object, name); err != nil {
		t.Fatal(err)
	}
	if _, err := status.SetCondition(object,
		metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled"},
		time.Unix(1_800_000_000, 0).UTC()); err != nil {
		t.Fatal(err)
	}

	removed, err := status.RemoveCondition(object, "Ready")
	if err != nil || !removed || len(object.conditions) != 0 {
		t.Fatalf("RemoveCondition() = (%v, %v), conditions=%#v", removed, err, object.conditions)
	}
	released, err := finalizers.Remove(object, name)
	if err != nil || !released || finalizers.Contains(object, name) {
		t.Fatalf("Remove() = (%v, %v), finalizers=%#v", released, err, object.GetFinalizers())
	}
}

// A controller may not adopt an object in another namespace: the API server
// would garbage-collect the child the moment the reference failed to resolve.
func TestControllerCannotAdoptAcrossNamespaces(t *testing.T) {
	value := namespacedScheme(t)
	if _, err := ownerrefs.SetController(run("run", "one"), stage("stage", "two"), value); err == nil {
		t.Fatal("SetController() across namespaces returned nil")
	}
}

// Two controllers cannot own the same object. The second adoption must fail
// rather than silently replace the first.
func TestSecondControllerIsRefused(t *testing.T) {
	value := namespacedScheme(t)
	object := stage("stage", "tenant")
	if _, err := ownerrefs.SetController(run("run-a", "tenant"), object, value); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerrefs.SetController(run("run-b", "tenant"), object, value); err == nil {
		t.Fatal("second SetController() returned nil")
	}
}

// A finalizer name must be domain-qualified. An unqualified one is accepted by
// the API server but is unowned, so nothing will ever remove it and the object
// cannot be deleted.
func TestFinalizerNamesMustBeQualified(t *testing.T) {
	for _, value := range []string{"", "cleanup", "mindclade.dev/"} {
		if _, err := finalizers.Parse(value); err == nil {
			t.Fatalf("Parse(%q) returned nil", value)
		}
	}
}

type stubWatcher struct {
	events  chan k8swatch.Event
	stopped bool
}

func (watcher *stubWatcher) Stop()                             { watcher.stopped = true }
func (watcher *stubWatcher) ResultChan() <-chan k8swatch.Event { return watcher.events }

// Until is how a reconciler waits for a specific transition. It must stop the
// watcher it was handed, on both the matching and the cancelled path, or the
// API server keeps the connection open for the life of the process.
func TestUntilStopsTheWatcherOnMatch(t *testing.T) {
	watcher := &stubWatcher{events: make(chan k8swatch.Event, 1)}
	watcher.events <- k8swatch.Event{Type: k8swatch.Modified, Object: stage("stage", "tenant")}

	event, err := watch.Until(context.Background(), watcher, watch.Options{},
		func(event k8swatch.Event) (bool, error) { return event.Type == k8swatch.Modified, nil })
	if err != nil {
		t.Fatalf("Until() = %v", err)
	}
	if event.Type != k8swatch.Modified {
		t.Fatalf("event=%#v", event)
	}
	if !watcher.stopped {
		t.Fatal("Until() left the watcher open")
	}
}

// A closed watch is not a successful one. Consume must report it so the caller
// re-establishes the watch instead of treating the stream as drained.
func TestConsumeReportsAClosedWatch(t *testing.T) {
	watcher := &stubWatcher{events: make(chan k8swatch.Event)}
	close(watcher.events)

	err := watch.Consume(context.Background(), watcher, watch.Options{},
		func(context.Context, k8swatch.Event) error { return nil })
	if !errors.Is(err, watch.ErrClosed) {
		t.Fatalf("Consume() = %v, want ErrClosed", err)
	}
	if !watcher.stopped {
		t.Fatal("Consume() left the watcher open")
	}
}

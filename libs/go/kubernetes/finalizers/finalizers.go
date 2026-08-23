// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package finalizers

import (
	"context"
	"reflect"
	"strings"

	validation "k8s.io/apimachinery/pkg/util/validation"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/kubernetes"
)

// Name is a validated domain-qualified finalizer name.
type Name string

func Parse(value string) (Name, error) {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, "/") || len(validation.IsQualifiedName(value)) != 0 {
		return "", faults.New(
			faults.CodeInvalidArgument,
			"invalid Kubernetes finalizer name",
			faults.WithReason("invalid_finalizer_name"),
			faults.WithOperation("kubernetes.finalizers.Parse"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return Name(value), nil
}

func MustParse(value string) Name {
	name, err := Parse(value)
	if err != nil {
		panic(err)
	}
	return name
}

func (name Name) String() string { return string(name) }

// Valid reports whether the name is safe to write to a Kubernetes object
// exactly as it is. Parse trims, so a parse-only check accepted
// Name(" mindclade.dev/cleanup ") while String kept the padding; Add then
// wrote a whitespace-padded finalizer that the API server rejects and that
// Contains can never match. Requiring the value to already equal its parsed
// form makes Valid mean "normalized", which is what every caller assumes.
func (name Name) Valid() bool {
	parsed, err := Parse(string(name))
	return err == nil && parsed == name
}

func Contains(object crclient.Object, name Name) bool {
	return !nilInterface(object) && name.Valid() && controllerutil.ContainsFinalizer(object, name.String())
}

// Add mutates object in memory and reports whether it changed.
func Add(object crclient.Object, name Name) (bool, error) {
	if nilInterface(object) {
		return false, invalid("Kubernetes object is required", "nil_kubernetes_object", "kubernetes.finalizers.Add")
	}
	if !name.Valid() {
		return false, invalid("valid finalizer name is required", "invalid_finalizer_name", "kubernetes.finalizers.Add")
	}
	if controllerutil.ContainsFinalizer(object, name.String()) {
		return false, nil
	}
	controllerutil.AddFinalizer(object, name.String())
	return true, nil
}

// Remove mutates object in memory and reports whether it changed.
func Remove(object crclient.Object, name Name) (bool, error) {
	if nilInterface(object) {
		return false, invalid("Kubernetes object is required", "nil_kubernetes_object", "kubernetes.finalizers.Remove")
	}
	if !name.Valid() {
		return false, invalid("valid finalizer name is required", "invalid_finalizer_name", "kubernetes.finalizers.Remove")
	}
	if !controllerutil.ContainsFinalizer(object, name.String()) {
		return false, nil
	}
	controllerutil.RemoveFinalizer(object, name.String())
	return true, nil
}

// Ensure adds name and persists the change with an optimistic-lock merge
// patch. A resource-version conflict is returned as a retryable Mindclade
// conflict fault by kubernetes.Qualify.
func Ensure(ctx context.Context, client crclient.Client, object crclient.Object, name Name, options ...crclient.PatchOption) (bool, error) {
	return persist(ctx, client, object, name, true, options...)
}

// EnsureAbsent removes name and persists the change with an optimistic-lock
// merge patch.
func EnsureAbsent(ctx context.Context, client crclient.Client, object crclient.Object, name Name, options ...crclient.PatchOption) (bool, error) {
	return persist(ctx, client, object, name, false, options...)
}

func persist(ctx context.Context, client crclient.Client, object crclient.Object, name Name, present bool, options ...crclient.PatchOption) (bool, error) {
	operation := "kubernetes.finalizers.Ensure"
	if !present {
		operation = "kubernetes.finalizers.EnsureAbsent"
	}
	if ctx == nil || nilInterface(client) || nilInterface(object) {
		return false, invalid("invalid finalizer persistence request", "invalid_finalizer_request", operation)
	}
	originalFinalizers := append([]string(nil), object.GetFinalizers()...)
	before, ok := object.DeepCopyObject().(crclient.Object)
	if !ok || nilInterface(before) {
		return false, faults.New(
			faults.CodeInternal,
			"Kubernetes object cannot be deep-copied",
			faults.WithReason("object_deep_copy_failed"),
			faults.WithOperation(operation),
			faults.WithContextMetadata(ctx),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	var changed bool
	var err error
	if present {
		changed, err = Add(object, name)
	} else {
		changed, err = Remove(object, name)
	}
	if err != nil || !changed {
		return changed, err
	}
	patch := crclient.MergeFromWithOptions(before, crclient.MergeFromWithOptimisticLock{})
	if err := client.Patch(ctx, object, patch, options...); err != nil {
		object.SetFinalizers(originalFinalizers)
		reference := kubernetes.ReferenceFor(object, object.GetObjectKind().GroupVersionKind())
		return false, kubernetes.QualifyObject(ctx, err, operation, reference, faults.Fields{"finalizer": name.String()})
	}
	return true, nil
}

func invalid(message, reason, operation string) error {
	return faults.New(
		faults.CodeInvalidArgument,
		message,
		faults.WithReason(reason),
		faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

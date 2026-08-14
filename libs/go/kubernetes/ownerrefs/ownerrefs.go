// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package ownerrefs

import (
	"errors"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"mindclade.internal/libs/go/faults"
)

func SetController(owner, controlled crclient.Object, scheme *runtime.Scheme, options ...controllerutil.OwnerReferenceOption) (bool, error) {
	if err := validate(owner, controlled, scheme, "kubernetes.ownerrefs.SetController"); err != nil {
		return false, err
	}
	before := appendOwnerReferences(controlled.GetOwnerReferences())
	if err := controllerutil.SetControllerReference(owner, controlled, scheme, options...); err != nil {
		var alreadyOwned *controllerutil.AlreadyOwnedError
		if errors.As(err, &alreadyOwned) {
			return false, faults.Wrap(
				err,
				faults.CodeConflict,
				"Kubernetes object already has a controller owner",
				faults.WithReason("controller_owner_conflict"),
				faults.WithOperation("kubernetes.ownerrefs.SetController"),
				faults.WithRetryPolicy(faults.NoRetry()),
			)
		}
		return false, faults.Wrap(err, faults.CodeInvalidArgument, "unable to set Kubernetes controller owner", faults.WithReason("invalid_controller_owner_reference"), faults.WithOperation("kubernetes.ownerrefs.SetController"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return !reflect.DeepEqual(before, controlled.GetOwnerReferences()), nil
}

func SetOwner(owner, object crclient.Object, scheme *runtime.Scheme, options ...controllerutil.OwnerReferenceOption) (bool, error) {
	if err := validate(owner, object, scheme, "kubernetes.ownerrefs.SetOwner"); err != nil {
		return false, err
	}
	before := appendOwnerReferences(object.GetOwnerReferences())
	if err := controllerutil.SetOwnerReference(owner, object, scheme, options...); err != nil {
		return false, faults.Wrap(err, faults.CodeInvalidArgument, "unable to set Kubernetes owner", faults.WithReason("invalid_owner_reference"), faults.WithOperation("kubernetes.ownerrefs.SetOwner"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return !reflect.DeepEqual(before, object.GetOwnerReferences()), nil
}

func RemoveOwner(owner, object crclient.Object, scheme *runtime.Scheme) (bool, error) {
	if err := validate(owner, object, scheme, "kubernetes.ownerrefs.RemoveOwner"); err != nil {
		return false, err
	}
	before := appendOwnerReferences(object.GetOwnerReferences())
	if err := controllerutil.RemoveOwnerReference(owner, object, scheme); err != nil {
		return false, faults.Wrap(err, faults.CodeInvalidArgument, "unable to remove Kubernetes owner", faults.WithReason("invalid_owner_reference"), faults.WithOperation("kubernetes.ownerrefs.RemoveOwner"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return !reflect.DeepEqual(before, object.GetOwnerReferences()), nil
}

func RemoveController(owner, object crclient.Object, scheme *runtime.Scheme) (bool, error) {
	if err := validate(owner, object, scheme, "kubernetes.ownerrefs.RemoveController"); err != nil {
		return false, err
	}
	before := appendOwnerReferences(object.GetOwnerReferences())
	if err := controllerutil.RemoveControllerReference(owner, object, scheme); err != nil {
		return false, faults.Wrap(err, faults.CodeInvalidArgument, "unable to remove Kubernetes controller owner", faults.WithReason("invalid_controller_owner_reference"), faults.WithOperation("kubernetes.ownerrefs.RemoveController"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return !reflect.DeepEqual(before, object.GetOwnerReferences()), nil
}

func validate(owner, object crclient.Object, scheme *runtime.Scheme, operation string) error {
	if nilInterface(owner) || nilInterface(object) || scheme == nil {
		return faults.New(faults.CodeInvalidArgument, "owner, object, and scheme are required", faults.WithReason("invalid_owner_reference_request"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	ownerNamespace := owner.GetNamespace()
	objectNamespace := object.GetNamespace()
	if ownerNamespace != "" && ownerNamespace != objectNamespace {
		return faults.New(
			faults.CodeInvalidArgument,
			"namespaced Kubernetes owner references cannot cross namespaces",
			faults.WithReason("cross_namespace_owner_reference"),
			faults.WithOperation(operation),
			faults.WithFields(faults.Fields{"owner_namespace": ownerNamespace, "object_namespace": objectNamespace}),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return nil
}

func appendOwnerReferences(values []metav1.OwnerReference) []metav1.OwnerReference {
	return append([]metav1.OwnerReference(nil), values...)
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

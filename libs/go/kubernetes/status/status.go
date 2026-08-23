// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package status

import (
	"context"
	"reflect"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/kubernetes/conditions"
	kpatch "go.mindclade.dev/libs/go/kubernetes/patch"
)

// ConditionAccessor is implemented by custom-resource status values that own
// a standard []metav1.Condition field.
type ConditionAccessor interface {
	GetConditions() []metav1.Condition
	SetConditions([]metav1.Condition)
}

func SetCondition(accessor ConditionAccessor, condition metav1.Condition, now time.Time) (bool, error) {
	if isNil(accessor) {
		return false, invalid("condition accessor is required", "nil_condition_accessor")
	}
	values := append([]metav1.Condition(nil), accessor.GetConditions()...)
	changed, err := conditions.Set(&values, condition, now)
	if err != nil || !changed {
		return changed, err
	}
	accessor.SetConditions(values)
	return true, nil
}

func RemoveCondition(accessor ConditionAccessor, conditionType string) (bool, error) {
	if isNil(accessor) {
		return false, invalid("condition accessor is required", "nil_condition_accessor")
	}
	values := append([]metav1.Condition(nil), accessor.GetConditions()...)
	if !conditions.Remove(&values, conditionType) {
		return false, nil
	}
	accessor.SetConditions(values)
	return true, nil
}

// Patch persists a previously mutated status using an optimistic-lock merge
// patch. before must be a deep copy taken before status mutation.
func Patch(ctx context.Context, client crclient.Client, before, after crclient.Object, options ...crclient.SubResourcePatchOption) error {
	return kpatch.Status(ctx, client, before, after, options...)
}

func invalid(message, reason string) error {
	return faults.New(faults.CodeInvalidArgument, message, faults.WithReason(reason), faults.WithOperation("kubernetes.status"), faults.WithRetryPolicy(faults.NoRetry()))
}

// ConditionAccessor is an interface, so a nil *T argument is not == nil: the
// interface value carries a type and a nil pointer. A plain `accessor == nil`
// guard let that through and the first GetConditions call crashed the
// reconciler. Every other package in this subtree uses the same reflective
// check for exactly this reason.
func isNil(value any) bool {
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

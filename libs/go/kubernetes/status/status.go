// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package status

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/kubernetes/conditions"
	kpatch "mindclade.internal/libs/go/kubernetes/patch"
)

// ConditionAccessor is implemented by custom-resource status values that own
// a standard []metav1.Condition field.
type ConditionAccessor interface {
	GetConditions() []metav1.Condition
	SetConditions([]metav1.Condition)
}

func SetCondition(accessor ConditionAccessor, condition metav1.Condition, now time.Time) (bool, error) {
	if accessor == nil {
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
	if accessor == nil {
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

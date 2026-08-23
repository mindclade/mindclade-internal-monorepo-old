// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package status

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.mindclade.dev/libs/go/faults"
)

type accessor struct{ values []metav1.Condition }

func (value *accessor) GetConditions() []metav1.Condition { return value.values }
func (value *accessor) SetConditions(values []metav1.Condition) {
	value.values = append([]metav1.Condition(nil), values...)
}

func TestSetAndRemoveCondition(t *testing.T) {
	value := &accessor{}
	condition := metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled"}
	changed, err := SetCondition(value, condition, time.Unix(100, 0))
	if err != nil || !changed || len(value.values) != 1 {
		t.Fatalf("SetCondition() = (%v, %v), values=%#v", changed, err, value.values)
	}
	changed, err = RemoveCondition(value, "Ready")
	if err != nil || !changed || len(value.values) != 0 {
		t.Fatalf("RemoveCondition() = (%v, %v), values=%#v", changed, err, value.values)
	}
}

// ConditionAccessor is an interface, so a nil *T argument is not == nil. The
// guard used to miss that and dereference the typed nil inside GetConditions,
// crashing the reconciler instead of returning an invalid-argument fault.
func TestConditionMutatorsRejectTypedNilAccessor(t *testing.T) {
	var value *accessor
	condition := metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled"}
	changed, err := SetCondition(value, condition, time.Unix(100, 0))
	if !faults.IsCode(err, faults.CodeInvalidArgument) || changed {
		t.Fatalf("SetCondition(typed nil) = (%v, %v)", changed, err)
	}
	changed, err = RemoveCondition(value, "Ready")
	if !faults.IsCode(err, faults.CodeInvalidArgument) || changed {
		t.Fatalf("RemoveCondition(typed nil) = (%v, %v)", changed, err)
	}
}

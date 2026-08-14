// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package status

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

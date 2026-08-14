// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package conditions

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetPreservesTransitionTimeWhenStatusDoesNotChange(t *testing.T) {
	first := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	var values []metav1.Condition
	condition := metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Starting"}
	changed, err := Set(&values, condition, first)
	if err != nil || !changed {
		t.Fatalf("first Set() = (%v, %v)", changed, err)
	}
	transition := values[0].LastTransitionTime
	condition.Reason = "Waiting"
	changed, err = Set(&values, condition, second)
	if err != nil || !changed {
		t.Fatalf("second Set() = (%v, %v)", changed, err)
	}
	if !values[0].LastTransitionTime.Equal(&transition) {
		t.Fatalf("transition time changed: %s", values[0].LastTransitionTime)
	}
	condition.Status = metav1.ConditionTrue
	condition.Reason = "Available"
	_, err = Set(&values, condition, second)
	if err != nil {
		t.Fatal(err)
	}
	if !values[0].LastTransitionTime.Time.Equal(second) {
		t.Fatalf("transition time = %s, want %s", values[0].LastTransitionTime.Time, second)
	}
}

func TestSetSortsByType(t *testing.T) {
	now := time.Now()
	values := []metav1.Condition{}
	for _, conditionType := range []string{"Ready", "Accepted"} {
		_, err := Set(&values, metav1.Condition{Type: conditionType, Status: metav1.ConditionTrue, Reason: "Valid"}, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	if values[0].Type != "Accepted" || values[1].Type != "Ready" {
		t.Fatalf("conditions not sorted: %#v", values)
	}
}

func TestConstructorsAndTransitionTime(t *testing.T) {
	condition, err := True("Ready", "Reconciled", "resource is ready", 7)
	if err != nil {
		t.Fatal(err)
	}
	var values []metav1.Condition
	first := time.Unix(100, 0).UTC()
	changed, err := Set(&values, condition, first)
	if err != nil || !changed {
		t.Fatalf("Set() = (%v, %v)", changed, err)
	}
	transition := values[0].LastTransitionTime
	condition.Message = "still ready"
	changed, err = Set(&values, condition, first.Add(time.Hour))
	if err != nil || !changed {
		t.Fatalf("second Set() = (%v, %v)", changed, err)
	}
	if !values[0].LastTransitionTime.Equal(&transition) {
		t.Fatalf("transition time changed: %v -> %v", transition, values[0].LastTransitionTime)
	}
}

func TestSetCanonicalizesDuplicateTypes(t *testing.T) {
	first := metav1.NewTime(time.Unix(100, 0).UTC())
	values := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Available", LastTransitionTime: first},
		{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Duplicate"},
	}
	changed, err := Set(&values, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled"}, time.Unix(200, 0).UTC())
	if err != nil || !changed {
		t.Fatalf("Set() = (%v, %v)", changed, err)
	}
	if len(values) != 1 || values[0].Type != "Ready" || !values[0].LastTransitionTime.Equal(&first) {
		t.Fatalf("conditions = %#v", values)
	}
}

func TestRemoveDeletesAllDuplicates(t *testing.T) {
	values := []metav1.Condition{{Type: "Ready"}, {Type: "Accepted"}, {Type: "Ready"}}
	if !Remove(&values, "Ready") {
		t.Fatal("Remove() = false")
	}
	if len(values) != 1 || values[0].Type != "Accepted" {
		t.Fatalf("conditions = %#v", values)
	}
}

func TestFindAndStatusPredicates(t *testing.T) {
	values := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Available"},
		{Type: "Degraded", Status: metav1.ConditionFalse, Reason: "Healthy"},
		{Type: "Accepted", Status: metav1.ConditionUnknown, Reason: "Pending"},
	}
	condition, ok := Find(values, "Ready")
	if !ok || condition.Reason != "Available" {
		t.Fatalf("Find() = (%#v, %v)", condition, ok)
	}
	if !IsTrue(values, "Ready") || !IsFalse(values, "Degraded") || !IsUnknown(values, "Accepted") {
		t.Fatal("status predicates returned false")
	}
	if _, ok := Find(values, "Missing"); ok || IsTrue(values, "Missing") {
		t.Fatal("missing condition reported present")
	}
}

func TestFalseAndUnknownConstructors(t *testing.T) {
	condition, err := False("Ready", "Unavailable", "not ready", 3)
	if err != nil || condition.Status != metav1.ConditionFalse {
		t.Fatalf("False() = (%#v, %v)", condition, err)
	}
	condition, err = Unknown("Ready", "Checking", "checking", 3)
	if err != nil || condition.Status != metav1.ConditionUnknown {
		t.Fatalf("Unknown() = (%#v, %v)", condition, err)
	}
}

func TestValidationFailuresDoNotMutate(t *testing.T) {
	original := []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Available"}}
	values := append([]metav1.Condition(nil), original...)
	invalid := []metav1.Condition{
		{Type: "bad type", Status: metav1.ConditionTrue, Reason: "Valid"},
		{Type: "Ready", Status: "Invalid", Reason: "Valid"},
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "bad-reason"},
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Valid", ObservedGeneration: -1},
	}
	for index, condition := range invalid {
		if changed, err := Set(&values, condition, time.Unix(100, 0)); err == nil || changed {
			t.Fatalf("invalid condition %d = (%v, %v)", index, changed, err)
		}
		if len(values) != 1 || values[0] != original[0] {
			t.Fatalf("invalid condition %d mutated destination: %#v", index, values)
		}
	}
	if changed, err := Set(nil, original[0], time.Now()); err == nil || changed {
		t.Fatalf("nil destination = (%v, %v)", changed, err)
	}
	if changed, err := Set(&values, original[0], time.Time{}); err == nil || changed {
		t.Fatalf("zero time = (%v, %v)", changed, err)
	}
}

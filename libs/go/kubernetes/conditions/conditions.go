// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package conditions

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	validation "k8s.io/apimachinery/pkg/util/validation"

	"mindclade.internal/libs/go/faults"
)

var reasonPattern = regexp.MustCompile(`^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$`)

func Find(conditions []metav1.Condition, conditionType string) (metav1.Condition, bool) {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition, true
		}
	}
	return metav1.Condition{}, false
}

func IsTrue(conditions []metav1.Condition, conditionType string) bool {
	condition, ok := Find(conditions, conditionType)
	return ok && condition.Status == metav1.ConditionTrue
}

func IsFalse(conditions []metav1.Condition, conditionType string) bool {
	condition, ok := Find(conditions, conditionType)
	return ok && condition.Status == metav1.ConditionFalse
}

func IsUnknown(conditions []metav1.Condition, conditionType string) bool {
	condition, ok := Find(conditions, conditionType)
	return ok && condition.Status == metav1.ConditionUnknown
}

// Set inserts or replaces a condition and returns whether the slice changed.
// LastTransitionTime changes only when the condition status changes.
func Set(target *[]metav1.Condition, desired metav1.Condition, now time.Time) (bool, error) {
	if target == nil {
		return false, invalid("condition destination is required", "nil_condition_destination", nil)
	}
	if err := Validate(desired); err != nil {
		return false, err
	}
	if now.IsZero() {
		return false, invalid("condition timestamp is required", "zero_condition_timestamp", nil)
	}

	now = now.UTC()
	before := append([]metav1.Condition(nil), (*target)...)
	updated := make([]metav1.Condition, 0, len(before)+1)
	var previous *metav1.Condition
	for index := range before {
		if before[index].Type == desired.Type {
			if previous == nil {
				copy := before[index]
				previous = &copy
			}
			continue
		}
		updated = append(updated, before[index])
	}
	if previous != nil && previous.Status == desired.Status && !previous.LastTransitionTime.IsZero() {
		desired.LastTransitionTime = previous.LastTransitionTime
	} else {
		desired.LastTransitionTime = metav1.NewTime(now)
	}
	updated = append(updated, desired)
	sort.SliceStable(updated, func(left, right int) bool {
		return updated[left].Type < updated[right].Type
	})
	if reflect.DeepEqual(before, updated) {
		return false, nil
	}
	*target = updated
	return true, nil
}

func Remove(target *[]metav1.Condition, conditionType string) bool {
	if target == nil {
		return false
	}
	conditionType = strings.TrimSpace(conditionType)
	before := *target
	updated := make([]metav1.Condition, 0, len(before))
	for _, condition := range before {
		if condition.Type != conditionType {
			updated = append(updated, condition)
		}
	}
	if len(updated) == len(before) {
		return false
	}
	if len(updated) == 0 {
		*target = nil
	} else {
		*target = updated
	}
	return true
}

func Validate(condition metav1.Condition) error {
	if problems := validation.IsQualifiedName(condition.Type); len(problems) != 0 {
		return invalid("invalid Kubernetes condition type", "invalid_condition_type", faults.Fields{"type": condition.Type, "problems": problems})
	}
	switch condition.Status {
	case metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown:
	default:
		return invalid("invalid Kubernetes condition status", "invalid_condition_status", faults.Fields{"type": condition.Type, "status": condition.Status})
	}
	if len(condition.Reason) == 0 || len(condition.Reason) > 1024 || !reasonPattern.MatchString(condition.Reason) {
		return invalid("invalid Kubernetes condition reason", "invalid_condition_reason", faults.Fields{"type": condition.Type})
	}
	if condition.ObservedGeneration < 0 {
		return invalid("invalid Kubernetes observed generation", "invalid_observed_generation", faults.Fields{"type": condition.Type})
	}
	if len(condition.Message) > 32768 {
		return invalid("Kubernetes condition message is too large", "condition_message_too_large", faults.Fields{"type": condition.Type, "maximum_bytes": 32768})
	}
	return nil
}

func invalid(message, reason string, fields faults.Fields) error {
	return faults.New(
		faults.CodeInvalidArgument,
		message,
		faults.WithReason(reason),
		faults.WithOperation("kubernetes.conditions"),
		faults.WithFields(fields),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

// New constructs a validated condition without assigning LastTransitionTime.
// Set owns transition-time semantics so callers cannot accidentally reset the
// timestamp on every reconciliation.
func New(conditionType string, status metav1.ConditionStatus, reason, message string, observedGeneration int64) (metav1.Condition, error) {
	condition := metav1.Condition{
		Type:               strings.TrimSpace(conditionType),
		Status:             status,
		ObservedGeneration: observedGeneration,
		Reason:             strings.TrimSpace(reason),
		Message:            message,
	}
	if err := Validate(condition); err != nil {
		return metav1.Condition{}, err
	}
	return condition, nil
}

func True(conditionType, reason, message string, observedGeneration int64) (metav1.Condition, error) {
	return New(conditionType, metav1.ConditionTrue, reason, message, observedGeneration)
}

func False(conditionType, reason, message string, observedGeneration int64) (metav1.Condition, error) {
	return New(conditionType, metav1.ConditionFalse, reason, message, observedGeneration)
}

func Unknown(conditionType, reason, message string, observedGeneration int64) (metav1.Condition, error) {
	return New(conditionType, metav1.ConditionUnknown, reason, message, observedGeneration)
}

// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package metadata

import (
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	validation "k8s.io/apimachinery/pkg/util/validation"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/requestmeta"
)

const (
	ManagedByLabel        = "app.kubernetes.io/managed-by"
	ComponentLabel        = "mindclade.dev/component"
	VersionLabel          = "mindclade.dev/version"
	QualificationLabel    = "mindclade.dev/qualification"
	RequestIDAnnotation   = "mindclade.dev/request-id"
	CorrelationAnnotation = "mindclade.dev/correlation-id"
)

const maximumAnnotationBytes = 256 * 1024

// Clone returns an independent copy of values.
func Clone(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

// Merge overlays non-empty keys from overlays from left to right.
func Merge(base map[string]string, overlays ...map[string]string) map[string]string {
	merged := Clone(base)
	if merged == nil {
		merged = map[string]string{}
	}
	for _, overlay := range overlays {
		for key, value := range overlay {
			if key == "" {
				continue
			}
			merged[key] = value
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// SortedKeys returns keys in lexical order.
func SortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func ValidateLabels(labels map[string]string) error {
	for key, value := range labels {
		if problems := validation.IsQualifiedName(key); len(problems) != 0 {
			return invalid("invalid Kubernetes label key", "invalid_label_key", key, problems)
		}
		if problems := validation.IsValidLabelValue(value); len(problems) != 0 {
			return invalid("invalid Kubernetes label value", "invalid_label_value", key, problems)
		}
	}
	return nil
}

func ValidateAnnotations(annotations map[string]string) error {
	total := 0
	for key, value := range annotations {
		if problems := validation.IsQualifiedName(key); len(problems) != 0 {
			return invalid("invalid Kubernetes annotation key", "invalid_annotation_key", key, problems)
		}
		if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
			return invalid("invalid Kubernetes annotation value", "invalid_annotation_value", key, []string{"must be valid UTF-8 and must not contain NUL"})
		}
		total += len(key) + len(value)
		if total > maximumAnnotationBytes {
			return faults.New(
				faults.CodeResourceExhausted,
				"Kubernetes annotations exceed the configured limit",
				faults.WithReason("annotations_too_large"),
				faults.WithOperation("kubernetes.metadata.ValidateAnnotations"),
				faults.WithField("maximum_bytes", maximumAnnotationBytes),
				faults.WithRetryPolicy(faults.NoRetry()),
			)
		}
	}
	return nil
}

func SetLabel(object metav1.Object, key, value string) (bool, error) {
	if isNil(object) {
		return false, nilObject("kubernetes.metadata.SetLabel")
	}
	candidate := Merge(object.GetLabels(), map[string]string{key: value})
	if err := ValidateLabels(candidate); err != nil {
		return false, err
	}
	if reflect.DeepEqual(candidate, object.GetLabels()) {
		return false, nil
	}
	object.SetLabels(candidate)
	return true, nil
}

func RemoveLabel(object metav1.Object, key string) (bool, error) {
	if isNil(object) {
		return false, nilObject("kubernetes.metadata.RemoveLabel")
	}
	labels := Clone(object.GetLabels())
	if _, exists := labels[key]; !exists {
		return false, nil
	}
	delete(labels, key)
	if len(labels) == 0 {
		labels = nil
	}
	object.SetLabels(labels)
	return true, nil
}

func SetAnnotation(object metav1.Object, key, value string) (bool, error) {
	if isNil(object) {
		return false, nilObject("kubernetes.metadata.SetAnnotation")
	}
	candidate := Merge(object.GetAnnotations(), map[string]string{key: value})
	if err := ValidateAnnotations(candidate); err != nil {
		return false, err
	}
	if reflect.DeepEqual(candidate, object.GetAnnotations()) {
		return false, nil
	}
	object.SetAnnotations(candidate)
	return true, nil
}

func RemoveAnnotation(object metav1.Object, key string) (bool, error) {
	if isNil(object) {
		return false, nilObject("kubernetes.metadata.RemoveAnnotation")
	}
	annotations := Clone(object.GetAnnotations())
	if _, exists := annotations[key]; !exists {
		return false, nil
	}
	delete(annotations, key)
	if len(annotations) == 0 {
		annotations = nil
	}
	object.SetAnnotations(annotations)
	return true, nil
}

func invalid(message, reason, key string, problems []string) error {
	return faults.New(
		faults.CodeInvalidArgument,
		message,
		faults.WithReason(reason),
		faults.WithOperation("kubernetes.metadata.Validate"),
		faults.WithFields(faults.Fields{"key": key, "problems": problems}),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

func nilObject(operation string) error {
	return faults.New(
		faults.CodeInvalidArgument,
		"Kubernetes object is required",
		faults.WithReason("nil_kubernetes_object"),
		faults.WithOperation(operation),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

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

// ManagedLabels returns the canonical labels shared by Mindclade-managed
// resources. Empty optional values are omitted.
func ManagedLabels(managedBy, component, version, qualification string) (map[string]string, error) {
	labels := map[string]string{}
	for key, value := range map[string]string{
		ManagedByLabel:     strings.TrimSpace(managedBy),
		ComponentLabel:     strings.TrimSpace(component),
		VersionLabel:       strings.TrimSpace(version),
		QualificationLabel: strings.TrimSpace(qualification),
	} {
		if value != "" {
			labels[key] = value
		}
	}
	if err := ValidateLabels(labels); err != nil {
		return nil, err
	}
	if len(labels) == 0 {
		return nil, nil
	}
	return labels, nil
}

// RequestAnnotations returns safe request-lineage annotations. The operation
// is intentionally omitted because Kubernetes metadata should not become a
// high-cardinality telemetry sink.
func RequestAnnotations(request requestmeta.Metadata) (map[string]string, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	annotations := map[string]string{}
	if !request.RequestID.IsZero() {
		annotations[RequestIDAnnotation] = request.RequestID.String()
	}
	if !request.CorrelationID.IsZero() {
		annotations[CorrelationAnnotation] = request.CorrelationID.String()
	}
	if err := ValidateAnnotations(annotations); err != nil {
		return nil, err
	}
	if len(annotations) == 0 {
		return nil, nil
	}
	return annotations, nil
}

// Apply merges labels and annotations in one validated mutation. The object is
// not changed when either candidate map is invalid.
func Apply(object metav1.Object, labels, annotations map[string]string) (bool, error) {
	if isNil(object) {
		return false, nilObject("kubernetes.metadata.Apply")
	}
	candidateLabels := Merge(object.GetLabels(), labels)
	candidateAnnotations := Merge(object.GetAnnotations(), annotations)
	if err := ValidateLabels(candidateLabels); err != nil {
		return false, err
	}
	if err := ValidateAnnotations(candidateAnnotations); err != nil {
		return false, err
	}
	changed := !reflect.DeepEqual(candidateLabels, object.GetLabels()) || !reflect.DeepEqual(candidateAnnotations, object.GetAnnotations())
	if !changed {
		return false, nil
	}
	object.SetLabels(candidateLabels)
	object.SetAnnotations(candidateAnnotations)
	return true, nil
}

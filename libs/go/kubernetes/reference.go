// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package kubernetes

import (
	"reflect"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"mindclade.internal/libs/go/faults"
)

// ObjectReference is a bounded diagnostic identity for a Kubernetes object.
// It intentionally excludes labels, annotations, spec, status, and managed
// fields so it is safe to attach to structured faults and telemetry.
type ObjectReference struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	UID        string
}

// ReferenceFor constructs a diagnostic reference from object and its resolved
// group/version/kind. A nil object produces a reference containing only GVK.
func ReferenceFor(object metav1.Object, gvk schema.GroupVersionKind) ObjectReference {
	reference := ObjectReference{
		APIVersion: strings.TrimSpace(gvk.GroupVersion().String()),
		Kind:       strings.TrimSpace(gvk.Kind),
	}
	if isNilObject(object) {
		return reference
	}
	reference.Namespace = strings.TrimSpace(object.GetNamespace())
	reference.Name = strings.TrimSpace(object.GetName())
	reference.UID = strings.TrimSpace(string(object.GetUID()))
	return reference
}

// Fields returns non-empty reference attributes using canonical field names.
func (reference ObjectReference) Fields() faults.Fields {
	fields := faults.Fields{}
	if reference.APIVersion != "" {
		fields[FieldAPIVersion] = reference.APIVersion
	}
	if reference.Kind != "" {
		fields[FieldKind] = reference.Kind
	}
	if reference.Namespace != "" {
		fields[FieldNamespace] = reference.Namespace
	}
	if reference.Name != "" {
		fields[FieldName] = reference.Name
	}
	if reference.UID != "" {
		fields[FieldUID] = reference.UID
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func isNilObject(value any) bool {
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

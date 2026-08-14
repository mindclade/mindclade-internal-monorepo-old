// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package metadata

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetAndRemoveLabel(t *testing.T) {
	object := &metav1.PartialObjectMetadata{}
	changed, err := SetLabel(object, ComponentLabel, "control-plane")
	if err != nil || !changed {
		t.Fatalf("SetLabel() = (%v, %v)", changed, err)
	}
	changed, err = SetLabel(object, ComponentLabel, "control-plane")
	if err != nil || changed {
		t.Fatalf("idempotent SetLabel() = (%v, %v)", changed, err)
	}
	changed, err = RemoveLabel(object, ComponentLabel)
	if err != nil || !changed {
		t.Fatalf("RemoveLabel() = (%v, %v)", changed, err)
	}
}

func TestValidateLabelsRejectsInvalidValue(t *testing.T) {
	if err := ValidateLabels(map[string]string{"mindclade.dev/component": "contains spaces"}); err == nil {
		t.Fatal("ValidateLabels() returned nil")
	}
}

func TestApplyIsAtomic(t *testing.T) {
	object := &metav1.PartialObjectMetadata{}
	changed, err := Apply(object, map[string]string{ComponentLabel: "control-plane"}, map[string]string{RequestIDAnnotation: "request_019c7af21b8276d2a0d522fe41739a21"})
	if err != nil || !changed {
		t.Fatalf("Apply() = (%v, %v)", changed, err)
	}
	before := Clone(object.GetLabels())
	changed, err = Apply(object, map[string]string{"bad key": "value"}, nil)
	if err == nil || changed {
		t.Fatalf("invalid Apply() = (%v, %v)", changed, err)
	}
	if !reflect.DeepEqual(before, object.GetLabels()) {
		t.Fatal("invalid Apply mutated object")
	}
}

func TestMergeDoesNotCanonicalizeInvalidKeys(t *testing.T) {
	merged := Merge(nil, map[string]string{" mindclade.dev/component": "control-plane"})
	if _, ok := merged[" mindclade.dev/component"]; !ok {
		t.Fatalf("Merge() canonicalized key: %#v", merged)
	}
	if err := ValidateLabels(merged); err == nil {
		t.Fatal("ValidateLabels() accepted whitespace-prefixed key")
	}
}

func TestValidateAnnotationsRejectsInvalidText(t *testing.T) {
	for _, value := range []string{"contains\x00nul", string([]byte{0xff})} {
		if err := ValidateAnnotations(map[string]string{RequestIDAnnotation: value}); err == nil {
			t.Fatalf("ValidateAnnotations(%q) returned nil", value)
		}
	}
}

func TestSortedKeysAndClone(t *testing.T) {
	values := map[string]string{"z": "last", "a": "first"}
	keys := SortedKeys(values)
	if !reflect.DeepEqual(keys, []string{"a", "z"}) {
		t.Fatalf("SortedKeys() = %#v", keys)
	}
	cloned := Clone(values)
	cloned["a"] = "changed"
	if values["a"] != "first" {
		t.Fatal("Clone aliases input")
	}
}

func TestSetAndRemoveAnnotation(t *testing.T) {
	object := &metav1.PartialObjectMetadata{}
	changed, err := SetAnnotation(object, RequestIDAnnotation, "request_019c7af21b8276d2a0d522fe41739a21")
	if err != nil || !changed {
		t.Fatalf("SetAnnotation() = (%v, %v)", changed, err)
	}
	changed, err = SetAnnotation(object, RequestIDAnnotation, "request_019c7af21b8276d2a0d522fe41739a21")
	if err != nil || changed {
		t.Fatalf("idempotent SetAnnotation() = (%v, %v)", changed, err)
	}
	changed, err = RemoveAnnotation(object, RequestIDAnnotation)
	if err != nil || !changed || object.GetAnnotations() != nil {
		t.Fatalf("RemoveAnnotation() = (%v, %v), annotations=%#v", changed, err, object.GetAnnotations())
	}
}

func TestManagedLabels(t *testing.T) {
	labels, err := ManagedLabels("mindclade", "control-plane", "v1", "qualified")
	if err != nil {
		t.Fatal(err)
	}
	if labels[ManagedByLabel] != "mindclade" || labels[ComponentLabel] != "control-plane" || labels[VersionLabel] != "v1" || labels[QualificationLabel] != "qualified" {
		t.Fatalf("labels = %#v", labels)
	}
	if labels, err := ManagedLabels("", "", "", ""); err != nil || labels != nil {
		t.Fatalf("empty labels = (%#v, %v)", labels, err)
	}
}

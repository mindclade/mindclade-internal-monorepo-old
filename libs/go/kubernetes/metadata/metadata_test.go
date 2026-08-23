// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package metadata

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.mindclade.dev/libs/go/faults"
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

// An empty key is not a "no-op key": it is an invalid key. Merge used to drop
// it silently, so these mutators reported (false, nil) — indistinguishable
// from "the label was already present" — and the caller's write vanished.
func TestSetLabelRejectsEmptyKey(t *testing.T) {
	object := &metav1.PartialObjectMetadata{}
	changed, err := SetLabel(object, "", "control-plane")
	if !faults.IsCode(err, faults.CodeInvalidArgument) || changed {
		t.Fatalf("SetLabel(empty key) = (%v, %v)", changed, err)
	}
	if object.GetLabels() != nil {
		t.Fatalf("labels = %#v", object.GetLabels())
	}
}

func TestSetAnnotationRejectsEmptyKey(t *testing.T) {
	object := &metav1.PartialObjectMetadata{}
	changed, err := SetAnnotation(object, "", "control-plane")
	if !faults.IsCode(err, faults.CodeInvalidArgument) || changed {
		t.Fatalf("SetAnnotation(empty key) = (%v, %v)", changed, err)
	}
	if object.GetAnnotations() != nil {
		t.Fatalf("annotations = %#v", object.GetAnnotations())
	}
}

func TestApplyRejectsEmptyKey(t *testing.T) {
	object := &metav1.PartialObjectMetadata{}
	if changed, err := Apply(object, map[string]string{"": "value"}, nil); !faults.IsCode(err, faults.CodeInvalidArgument) || changed {
		t.Fatalf("Apply(empty label key) = (%v, %v)", changed, err)
	}
	if changed, err := Apply(object, nil, map[string]string{"": "value"}); !faults.IsCode(err, faults.CodeInvalidArgument) || changed {
		t.Fatalf("Apply(empty annotation key) = (%v, %v)", changed, err)
	}
	if object.GetLabels() != nil || object.GetAnnotations() != nil {
		t.Fatalf("rejected Apply mutated object: labels=%#v annotations=%#v", object.GetLabels(), object.GetAnnotations())
	}
}

func TestRemoveRejectsEmptyKey(t *testing.T) {
	object := &metav1.PartialObjectMetadata{}
	changed, err := RemoveLabel(object, "")
	if !faults.IsCode(err, faults.CodeInvalidArgument) || changed {
		t.Fatalf("RemoveLabel(empty key) = (%v, %v)", changed, err)
	}
	changed, err = RemoveAnnotation(object, "")
	if !faults.IsCode(err, faults.CodeInvalidArgument) || changed {
		t.Fatalf("RemoveAnnotation(empty key) = (%v, %v)", changed, err)
	}
}

func TestRemoveAcceptsKeysThatAreNoLongerWritable(t *testing.T) {
	object := &metav1.PartialObjectMetadata{}
	object.SetLabels(map[string]string{"legacy key": "value"})
	changed, err := RemoveLabel(object, "legacy key")
	if err != nil || !changed || object.GetLabels() != nil {
		t.Fatalf("RemoveLabel(legacy key) = (%v, %v), labels=%#v", changed, err, object.GetLabels())
	}
}

func TestMergePreservesEmptyKeysForValidation(t *testing.T) {
	merged := Merge(nil, map[string]string{"": "value"})
	if _, ok := merged[""]; !ok {
		t.Fatalf("Merge() dropped the empty key: %#v", merged)
	}
	if err := ValidateLabels(merged); err == nil {
		t.Fatal("ValidateLabels() accepted an empty key")
	}
	if err := ValidateAnnotations(merged); err == nil {
		t.Fatal("ValidateAnnotations() accepted an empty key")
	}
}

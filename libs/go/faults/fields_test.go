// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package faults

import "testing"

func TestFieldsCloneIsDefensive(t *testing.T) {
	t.Parallel()

	original := Fields{
		"name": "run-1",
		"nested": map[string]any{
			"values": []any{"a", "b"},
		},
		"numbers": []int{1, 2, 3},
	}

	cloned := original.Clone()
	cloned["name"] = "changed"
	clonedNested := cloned["nested"].(Fields)
	clonedNested["values"].([]any)[0] = "changed"
	cloned["numbers"].([]int)[0] = 99

	if got := original["name"]; got != "run-1" {
		t.Fatalf("original name mutated: %v", got)
	}
	originalNested := original["nested"].(map[string]any)
	if got := originalNested["values"].([]any)[0]; got != "a" {
		t.Fatalf("original nested value mutated: %v", got)
	}
	if got := original["numbers"].([]int)[0]; got != 1 {
		t.Fatalf("original numbers mutated: %v", got)
	}
}

func TestFieldsRedactsSensitiveKeys(t *testing.T) {
	t.Parallel()

	fields := Fields{
		"api_key":       "secret-key",
		"Authorization": "Bearer token",
		"api_key_id":    "key_123",
		"nested": map[string]any{
			"access-token": "secret-token",
			"run_id":       "run_123",
		},
	}.Clone()

	if got := fields["api_key"]; got != RedactedValue {
		t.Fatalf("api_key = %v, want redacted", got)
	}
	if got := fields["Authorization"]; got != RedactedValue {
		t.Fatalf("Authorization = %v, want redacted", got)
	}
	if got := fields["api_key_id"]; got != "key_123" {
		t.Fatalf("api_key_id = %v, want preserved", got)
	}

	nested := fields["nested"].(Fields)
	if got := nested["access-token"]; got != RedactedValue {
		t.Fatalf("nested access-token = %v, want redacted", got)
	}
	if got := nested["run_id"]; got != "run_123" {
		t.Fatalf("nested run_id = %v, want preserved", got)
	}
}

func TestFieldsMergeDoesNotMutateInputs(t *testing.T) {
	t.Parallel()

	base := Fields{"a": 1, "shared": "base"}
	overlay := Fields{"b": 2, "shared": "overlay"}

	merged := base.Merge(overlay)
	merged["a"] = 100

	if got := base["a"]; got != 1 {
		t.Fatalf("base mutated: %v", got)
	}
	if got := overlay["shared"]; got != "overlay" {
		t.Fatalf("overlay mutated: %v", got)
	}
	if got := merged["shared"]; got != "overlay" {
		t.Fatalf("merged shared = %v, want overlay", got)
	}
}

func TestFieldsDropsBlankKeys(t *testing.T) {
	t.Parallel()

	fields := Fields{"   ": "ignored", "ok": true}.Clone()
	if len(fields) != 1 {
		t.Fatalf("len(fields) = %d, want 1", len(fields))
	}
	if got := fields["ok"]; got != true {
		t.Fatalf("fields[ok] = %v", got)
	}
}

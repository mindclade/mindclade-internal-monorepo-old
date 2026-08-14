// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package faults

import "strings"

const (
	FieldRequestID      = "request_id"
	FieldTraceID        = "trace_id"
	FieldTenantID       = "tenant_id"
	FieldOrganizationID = "organization_id"
	FieldResourceType   = "resource_type"
	FieldResourceID     = "resource_id"
	FieldModelID        = "model_id"
	FieldRunID          = "run_id"
	FieldOperation      = "operation"

	// RedactedValue replaces values assigned to recognized sensitive keys.
	RedactedValue = "[REDACTED]"
)

// Fields contains structured diagnostic metadata.
//
// Callers should use small, bounded, non-sensitive values. Fields are not a
// substitute for request payloads, model inputs, biological datasets, logs, or
// traces.
type Fields map[string]any

// Clone returns a defensively copied and redacted field map.
//
// Common JSON-like nested maps and slices are copied recursively. Values of
// unknown mutable types are retained as-is and should be treated as immutable.
func (fields Fields) Clone() Fields {
	return cloneFields(fields)
}

// Merge returns a new map containing fields overlaid by other. Neither input
// map is mutated. Values from other win when the same key exists in both maps.
func (fields Fields) Merge(other Fields) Fields {
	return mergeFields(fields, other)
}

func cloneFields(input Fields) Fields {
	if len(input) == 0 {
		return nil
	}

	output := make(Fields, len(input))
	for rawKey, value := range input {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}

		if isSensitiveFieldKey(key) {
			output[key] = RedactedValue
			continue
		}
		output[key] = cloneFieldValue(value)
	}

	if len(output) == 0 {
		return nil
	}
	return output
}

func mergeFields(base Fields, overlay Fields) Fields {
	output := cloneFields(base)
	if output == nil && len(overlay) > 0 {
		output = make(Fields, len(overlay))
	}

	for key, value := range cloneFields(overlay) {
		output[key] = value
	}

	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneFieldValue(value any) any {
	switch typed := value.(type) {
	case Fields:
		return cloneFields(typed)
	case map[string]any:
		return cloneFields(Fields(typed))
	case map[string]string:
		cloned := make(map[string]string, len(typed))
		for rawKey, nestedValue := range typed {
			key := strings.TrimSpace(rawKey)
			if key == "" {
				continue
			}
			if isSensitiveFieldKey(key) {
				cloned[key] = RedactedValue
			} else {
				cloned[key] = nestedValue
			}
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index, nestedValue := range typed {
			cloned[index] = cloneFieldValue(nestedValue)
		}
		return cloned
	case []Fields:
		cloned := make([]Fields, len(typed))
		for index, nestedValue := range typed {
			cloned[index] = cloneFields(nestedValue)
		}
		return cloned
	case []map[string]any:
		cloned := make([]map[string]any, len(typed))
		for index, nestedValue := range typed {
			cloned[index] = map[string]any(cloneFields(Fields(nestedValue)))
		}
		return cloned
	case []string:
		return cloneSlice(typed)
	case []byte:
		return cloneSlice(typed)
	case []bool:
		return cloneSlice(typed)
	case []int:
		return cloneSlice(typed)
	case []int32:
		return cloneSlice(typed)
	case []int64:
		return cloneSlice(typed)
	case []uint:
		return cloneSlice(typed)
	case []uint32:
		return cloneSlice(typed)
	case []uint64:
		return cloneSlice(typed)
	case []float32:
		return cloneSlice(typed)
	case []float64:
		return cloneSlice(typed)
	default:
		return value
	}
}

func cloneSlice[T any](input []T) []T {
	if input == nil {
		return nil
	}
	output := make([]T, len(input))
	copy(output, input)
	return output
}

func isSensitiveFieldKey(key string) bool {
	canonical := canonicalFieldKey(key)

	switch canonical {
	case "authorization",
		"proxy_authorization",
		"cookie",
		"set_cookie",
		"password",
		"passwd",
		"password_hash",
		"secret",
		"client_secret",
		"token",
		"access_token",
		"refresh_token",
		"id_token",
		"auth_token",
		"session_token",
		"api_key",
		"private_key",
		"raw_request_body",
		"request_body",
		"response_body",
		"model_input",
		"biological_input":
		return true
	}

	sensitiveSuffixes := [...]string{
		"_password",
		"_passwd",
		"_secret",
		"_token",
		"_api_key",
		"_private_key",
	}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(canonical, suffix) {
			return true
		}
	}

	return false
}

func canonicalFieldKey(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	replacer := strings.NewReplacer(
		"-", "_",
		".", "_",
		" ", "_",
	)
	return replacer.Replace(normalized)
}

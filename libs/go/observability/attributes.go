// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/requestmeta"
)

const (
	MaximumAttributeCount        = 64
	MaximumAttributeKeyLength    = 128
	MaximumAttributeStringLength = 4096
	MaximumAttributeDepth        = 4
	MaximumLabelCount            = 16
	MaximumLabelKeyLength        = 64
	MaximumLabelValueLength      = 128
)

// Attributes is an immutable, bounded set of structured telemetry attributes.
type Attributes struct{ fields faults.Fields }

// NewAttributes validates, redacts, bounds, and defensively copies fields.
func NewAttributes(fields faults.Fields) (Attributes, error) {
	if len(fields) == 0 {
		return Attributes{}, nil
	}
	cloned := fields.Clone()
	if len(cloned) > MaximumAttributeCount {
		return Attributes{}, invalidArgument(
			ErrInvalidAttributes,
			"too many observability attributes",
			"too_many_attributes",
			operationNewAttributes,
			faults.Fields{"attribute_count": len(cloned), "maximum": MaximumAttributeCount},
		)
	}
	normalized := make(faults.Fields, len(cloned))
	for key, value := range cloned {
		if !validAttributeKey(key) {
			return Attributes{}, invalidArgument(
				ErrInvalidAttributes,
				"invalid observability attribute key",
				"invalid_attribute_key",
				operationNewAttributes,
				faults.Fields{"attribute_key": key},
			)
		}
		converted, err := normalizeAttributeValue(value, 0)
		if err != nil {
			return Attributes{}, invalidArgument(
				errors.Join(ErrInvalidAttributes, err),
				"invalid observability attribute value",
				"invalid_attribute_value",
				operationNewAttributes,
				faults.Fields{"attribute_key": key},
			)
		}
		normalized[key] = converted
	}
	return Attributes{fields: normalized.Clone()}, nil
}

// MustAttributes constructs attributes or panics. It is intended for static
// declarations.
func MustAttributes(fields faults.Fields) Attributes {
	attributes, err := NewAttributes(fields)
	if err != nil {
		panic(err)
	}
	return attributes
}

func (attributes Attributes) IsZero() bool { return len(attributes.fields) == 0 }
func (attributes Attributes) Len() int     { return len(attributes.fields) }

// Fields returns a defensive copy.
func (attributes Attributes) Fields() faults.Fields { return attributes.fields.Clone() }

// Merge returns attributes overlaid by other. When the union exceeds the
// package bound, every overlay key is preserved and base-only keys are retained
// in lexical order until the limit is reached. This keeps request, trace, and
// call-site enrichment authoritative without allowing unbounded records.
func (attributes Attributes) Merge(other Attributes) Attributes {
	mergedFields := attributes.fields.Merge(other.fields)
	if len(mergedFields) <= MaximumAttributeCount {
		merged, _ := NewAttributes(mergedFields)
		return merged
	}

	bounded := other.fields.Clone()
	if bounded == nil {
		bounded = make(faults.Fields, MaximumAttributeCount)
	}
	baseKeys := make([]string, 0, len(attributes.fields))
	for key := range attributes.fields {
		if _, overlaid := other.fields[key]; !overlaid {
			baseKeys = append(baseKeys, key)
		}
	}
	sort.Strings(baseKeys)
	for _, key := range baseKeys {
		if len(bounded) >= MaximumAttributeCount {
			break
		}
		bounded[key] = attributes.fields[key]
	}
	merged, _ := NewAttributes(bounded)
	return merged
}

// SlogAttrs returns a deterministically ordered set of slog attributes.
func (attributes Attributes) SlogAttrs() []slog.Attr {
	keys := make([]string, 0, len(attributes.fields))
	for key := range attributes.fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]slog.Attr, 0, len(keys))
	for _, key := range keys {
		result = append(result, slog.Any(key, attributes.fields[key]))
	}
	return result
}

// ContextAttributes extracts bounded request lineage and non-PII principal
// identity from ctx.
func ContextAttributes(ctx context.Context) Attributes {
	if ctx == nil {
		return Attributes{}
	}
	contextValue := ctx
	fields := faults.Fields{}
	if metadata, ok := requestmeta.FromContext(contextValue); ok {
		if !metadata.RequestID.IsZero() {
			fields["request.id"] = metadata.RequestID.String()
		}
		if !metadata.CorrelationID.IsZero() {
			fields["request.correlation_id"] = metadata.CorrelationID.String()
		}
		if !metadata.CausationID.IsZero() {
			fields["request.causation_id"] = metadata.CausationID.String()
		}
		if !metadata.Operation.IsZero() {
			fields["operation.name"] = metadata.Operation.String()
		}
	}
	if principal, ok := auth.PrincipalFromContext(contextValue); ok {
		fields["principal.kind"] = principal.Kind().String()
		fields["principal.key"] = principal.Key()
		if !principal.ID().IsZero() {
			fields["principal.id"] = principal.ID().String()
		}
		if !principal.OrganizationID().IsZero() {
			fields["organization.id"] = principal.OrganizationID().String()
		}
		if !principal.TenantID().IsZero() {
			fields["tenant.id"] = principal.TenantID().String()
		}
	}
	attributes, _ := NewAttributes(fields)
	return attributes
}

// ErrorAttributes returns only externally safe failure information.
func ErrorAttributes(err error) Attributes {
	if err == nil {
		return Attributes{}
	}
	fields := faults.Fields{
		"error.code":    faults.CodeOf(err).String(),
		"error.message": faults.PublicMessageOf(err),
	}
	if reason := faults.ReasonOf(err); reason != "" {
		fields["error.reason"] = reason
	}
	if operation := faults.OperationOf(err); operation != "" {
		fields["error.operation"] = operation
	}
	if policy := faults.RetryPolicyOf(err); policy.Specified() {
		fields["error.retry_kind"] = string(policy.Kind)
	}
	attributes, _ := NewAttributes(fields)
	return attributes
}

func validAttributeKey(key string) bool {
	if key == "" || len(key) > MaximumAttributeKeyLength || strings.TrimSpace(key) != key {
		return false
	}
	previousSeparator := false
	for index := 0; index < len(key); index++ {
		character := key[index]
		letter := character >= 'a' && character <= 'z'
		digit := character >= '0' && character <= '9'
		separator := character == '.' || character == '_' || character == '-'
		if !letter && !digit && !separator || index == 0 && !letter || index == len(key)-1 && separator || separator && previousSeparator {
			return false
		}
		previousSeparator = separator
	}
	return true
}

func normalizeAttributeValue(value any, depth int) (any, error) {
	if depth > MaximumAttributeDepth {
		return nil, errors.New("attribute nesting exceeds maximum")
	}
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return truncateString(typed, MaximumAttributeStringLength), nil
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, time.Duration, time.Time:
		return typed, nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return nil, errors.New("non-finite float")
		}
		return typed, nil
	case error:
		return faults.PublicMessageOf(typed), nil
	case fmt.Stringer:
		return truncateString(typed.String(), MaximumAttributeStringLength), nil
	case []string:
		output := make([]string, len(typed))
		for index, item := range typed {
			output[index] = truncateString(item, MaximumAttributeStringLength)
		}
		return output, nil
	case []int:
		return append([]int(nil), typed...), nil
	case []int64:
		return append([]int64(nil), typed...), nil
	case []float64:
		output := append([]float64(nil), typed...)
		for _, item := range output {
			if math.IsNaN(item) || math.IsInf(item, 0) {
				return nil, errors.New("non-finite float")
			}
		}
		return output, nil
	case []bool:
		return append([]bool(nil), typed...), nil
	case faults.Fields:
		nested := make(faults.Fields, len(typed))
		for key, item := range typed.Clone() {
			if !validAttributeKey(key) {
				return nil, errors.New("invalid nested attribute key")
			}
			converted, err := normalizeAttributeValue(item, depth+1)
			if err != nil {
				return nil, err
			}
			nested[key] = converted
		}
		return nested, nil
	case map[string]any:
		return normalizeAttributeValue(faults.Fields(typed), depth)
	default:
		return truncateString(fmt.Sprint(typed), MaximumAttributeStringLength), nil
	}
}

func truncateString(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	if maximum <= 3 {
		return value[:maximum]
	}
	return value[:maximum-3] + "..."
}

// Labels is an immutable low-cardinality metric label set.
type Labels struct{ values map[string]string }

func NewLabels(values map[string]string) (Labels, error) {
	if len(values) == 0 {
		return Labels{}, nil
	}
	if len(values) > MaximumLabelCount {
		return Labels{}, invalidArgument(ErrInvalidLabels, "too many metric labels", "too_many_metric_labels", operationNewLabels, faults.Fields{"label_count": len(values)})
	}
	output := make(map[string]string, len(values))
	for key, value := range values {
		if !validAttributeKey(key) || len(key) > MaximumLabelKeyLength || sensitiveKey(key) {
			return Labels{}, invalidArgument(ErrInvalidLabels, "invalid metric label key", "invalid_metric_label_key", operationNewLabels, faults.Fields{"label_key": key})
		}
		normalized := strings.TrimSpace(value)
		if normalized == "" || len(normalized) > MaximumLabelValueLength {
			return Labels{}, invalidArgument(ErrInvalidLabels, "invalid metric label value", "invalid_metric_label_value", operationNewLabels, faults.Fields{"label_key": key, "value_length": len(normalized)})
		}
		output[key] = normalized
	}
	return Labels{values: output}, nil
}

func MustLabels(values map[string]string) Labels {
	labels, err := NewLabels(values)
	if err != nil {
		panic(err)
	}
	return labels
}

func (labels Labels) IsZero() bool { return len(labels.values) == 0 }
func (labels Labels) Len() int     { return len(labels.values) }
func (labels Labels) Map() map[string]string {
	if len(labels.values) == 0 {
		return nil
	}
	output := make(map[string]string, len(labels.values))
	for key, value := range labels.values {
		output[key] = value
	}
	return output
}
func (labels Labels) Merge(other Labels) Labels {
	merged := labels.Map()
	if merged == nil {
		merged = make(map[string]string, len(other.values))
	}
	for key, value := range other.values {
		merged[key] = value
	}
	if len(merged) > MaximumLabelCount {
		bounded := other.Map()
		if bounded == nil {
			bounded = make(map[string]string, MaximumLabelCount)
		}
		baseKeys := make([]string, 0, len(labels.values))
		for key := range labels.values {
			if _, overlaid := other.values[key]; !overlaid {
				baseKeys = append(baseKeys, key)
			}
		}
		sort.Strings(baseKeys)
		for _, key := range baseKeys {
			if len(bounded) >= MaximumLabelCount {
				break
			}
			bounded[key] = labels.values[key]
		}
		merged = bounded
	}
	result, _ := NewLabels(merged)
	return result
}

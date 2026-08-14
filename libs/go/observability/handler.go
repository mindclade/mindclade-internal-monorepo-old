// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package observability

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"mindclade.internal/libs/go/faults"
)

type contextHandler struct {
	next          slog.Handler
	static        Attributes
	traceProvider TraceContextProvider
}

func (handler *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.next.Enabled(ctx, level)
}

func (handler *contextHandler) Handle(ctx context.Context, record slog.Record) error {
	combined := handler.static.Merge(ContextAttributes(ctx))
	if trace, ok := safeTraceContext(ctx, handler.traceProvider); ok {
		combined = combined.Merge(trace.Attributes())
	}

	enriched := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	enriched.AddAttrs(combined.SlogAttrs()...)
	record.Attrs(func(attribute slog.Attr) bool {
		enriched.AddAttrs(attribute)
		return true
	})
	return handler.next.Handle(ctx, enriched)
}

func (handler *contextHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	return &contextHandler{
		next:          handler.next.WithAttrs(attributes),
		static:        handler.static,
		traceProvider: handler.traceProvider,
	}
}

func (handler *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{
		next:          handler.next.WithGroup(name),
		static:        handler.static,
		traceProvider: handler.traceProvider,
	}
}

func safeTraceContext(ctx context.Context, provider TraceContextProvider) (trace TraceContext, ok bool) {
	if nilInterface(provider) {
		return TraceContext{}, false
	}
	defer func() {
		if recover() != nil {
			trace = TraceContext{}
			ok = false
		}
	}()
	trace, ok = provider.TraceContext(ctx)
	if !ok || trace.Validate() != nil {
		return TraceContext{}, false
	}
	return trace, true
}

type redactingHandler struct{ next slog.Handler }

func (handler *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.next.Enabled(ctx, level)
}

func (handler *redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	sanitized := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attribute slog.Attr) bool {
		sanitized.AddAttrs(sanitizeAttr(attribute, 0))
		return true
	})
	return handler.next.Handle(ctx, sanitized)
}

func (handler *redactingHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	sanitized := make([]slog.Attr, 0, len(attributes))
	for _, attribute := range attributes {
		sanitized = append(sanitized, sanitizeAttr(attribute, 0))
	}
	return &redactingHandler{next: handler.next.WithAttrs(sanitized)}
}

func (handler *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{next: handler.next.WithGroup(name)}
}

func sanitizeAttr(attribute slog.Attr, depth int) slog.Attr {
	if attribute.Equal(slog.Attr{}) {
		return slog.Attr{}
	}
	if sensitiveKey(attribute.Key) {
		return slog.String(attribute.Key, faults.RedactedValue)
	}
	if depth > MaximumAttributeDepth {
		return slog.String(attribute.Key, "[MAX_DEPTH]")
	}

	value := resolveLogValue(attribute.Value)
	switch value.Kind() {
	case slog.KindString:
		return slog.String(attribute.Key, truncateString(value.String(), MaximumAttributeStringLength))
	case slog.KindAny:
		return sanitizeAny(attribute.Key, value.Any(), depth)
	case slog.KindGroup:
		group := value.Group()
		sanitized := make([]slog.Attr, 0, len(group))
		for _, nested := range group {
			sanitized = append(sanitized, sanitizeAttr(nested, depth+1))
		}
		return slog.Group(attribute.Key, attrsToAny(sanitized)...)
	default:
		attribute.Value = value
		return attribute
	}
}

func resolveLogValue(value slog.Value) (resolved slog.Value) {
	resolved = value
	defer func() {
		if recover() != nil {
			resolved = slog.StringValue("[LOG_VALUE_PANIC]")
		}
	}()
	for count := 0; count < 8 && resolved.Kind() == slog.KindLogValuer; count++ {
		resolved = resolved.Resolve()
	}
	if resolved.Kind() == slog.KindLogValuer {
		return slog.StringValue("[MAX_LOG_VALUE_DEPTH]")
	}
	return resolved
}

func sanitizeAny(key string, value any, depth int) slog.Attr {
	switch typed := value.(type) {
	case nil:
		return slog.Any(key, nil)
	case error:
		return slog.String(key, faults.PublicMessageOf(typed))
	case []byte:
		return slog.String(key, fmt.Sprintf("[%d bytes]", len(typed)))
	case faults.Fields:
		return slog.Group(key, fieldsToAny(typed, depth+1)...)
	case map[string]any:
		return slog.Group(key, fieldsToAny(faults.Fields(typed), depth+1)...)
	case map[string]string:
		fields := make(faults.Fields, len(typed))
		for nestedKey, nestedValue := range typed {
			fields[nestedKey] = nestedValue
		}
		return slog.Group(key, fieldsToAny(fields, depth+1)...)
	case fmt.Stringer:
		return slog.String(key, truncateString(typed.String(), MaximumAttributeStringLength))
	default:
		normalized, err := normalizeAttributeValue(typed, depth)
		if err != nil {
			return slog.String(key, "[INVALID_ATTRIBUTE]")
		}
		return slog.Any(key, normalized)
	}
}

func fieldsToAny(fields faults.Fields, depth int) []any {
	cloned := fields.Clone()
	keys := make([]string, 0, len(cloned))
	for key := range cloned {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]any, 0, len(keys))
	for _, key := range keys {
		values = append(values, sanitizeAttr(slog.Any(key, cloned[key]), depth))
	}
	return values
}

func attrsToAny(attributes []slog.Attr) []any {
	values := make([]any, len(attributes))
	for index, attribute := range attributes {
		values[index] = attribute
	}
	return values
}

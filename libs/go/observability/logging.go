// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package observability

import (
	"context"
	"io"
	"log/slog"

	"go.mindclade.dev/libs/go/faults"
)

type loggerContextKey struct{}

// ContextWithLogger stores logger in a child context.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) (context.Context, error) {
	if ctx == nil {
		return nil, invalidArgument(ErrNilContext, "nil logging context", "nil_context", "observability.ContextWithLogger", nil)
	}
	if logger == nil {
		return nil, invalidArgument(ErrNilHandler, "logger must not be nil", "nil_logger", "observability.ContextWithLogger", nil)
	}
	return context.WithValue(ctx, loggerContextKey{}, logger), nil
}

// LoggerFromContext retrieves a logger or returns fallback. When fallback is
// nil, a discard logger is returned.
func LoggerFromContext(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if ctx != nil {
		if logger, ok := ctx.Value(loggerContextKey{}).(*slog.Logger); ok && logger != nil {
			return logger
		}
	}
	if fallback != nil {
		return fallback
	}
	return DiscardLogger()
}

// DiscardLogger returns a logger that drops all records.
func DiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 100}))
}

// NewLogger builds a context-enriching, redacting logger around handler.
func NewLogger(handler slog.Handler, resource Resource, static Attributes, traceProvider TraceContextProvider) (*slog.Logger, error) {
	if nilInterface(handler) {
		return nil, invalidArgument(ErrNilHandler, "slog handler must not be nil", "nil_handler", "observability.NewLogger", nil)
	}
	if err := resource.Validate(); err != nil {
		return nil, err
	}
	if nilInterface(traceProvider) {
		traceProvider = noopTraceContextProvider{}
	}
	enriched := &contextHandler{
		next:          &redactingHandler{next: handler},
		static:        static.Merge(resource.Attributes()),
		traceProvider: traceProvider,
	}
	return slog.New(enriched), nil
}

// LogError emits a record containing only public fault information.
func LogError(ctx context.Context, logger *slog.Logger, level slog.Level, message string, err error, attributes ...slog.Attr) {
	if logger == nil {
		logger = DiscardLogger()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	safe := ErrorAttributes(err).SlogAttrs()
	safe = append(safe, attributes...)
	logger.LogAttrs(ctx, level, message, safe...)
}

// FieldsAttrs converts redacted fault fields into deterministic slog attrs.
func FieldsAttrs(fields faults.Fields) []slog.Attr {
	attributes, err := NewAttributes(fields)
	if err != nil {
		return []slog.Attr{slog.String("observability.attribute_error", faults.PublicMessageOf(err))}
	}
	return attributes.SlogAttrs()
}

// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package observability

import (
	"context"
	"log/slog"
	"testing"
)

func TestInterfaceAdaptersAndNilSafety(t *testing.T) {
	var metricCalls int
	MetricSinkFunc(func(context.Context, Measurement) { metricCalls++ }).Record(context.Background(), Measurement{})
	if metricCalls != 1 {
		t.Fatalf("metricCalls = %d", metricCalls)
	}

	var traceCalls int
	provider := TraceContextProviderFunc(func(context.Context) (TraceContext, bool) {
		traceCalls++
		return TraceContext{}, false
	})
	provider.TraceContext(context.Background())
	if traceCalls != 1 {
		t.Fatalf("traceCalls = %d", traceCalls)
	}

	var handled int
	ErrorHandlerFunc(func(error) { handled++ }).Handle(assertionError("failure"))
	if handled != 1 {
		t.Fatalf("handled = %d", handled)
	}

	if LoggerFromContext(nil, nil) == nil || !DiscardLogger().Enabled(context.Background(), slog.LevelError+100) {
		t.Fatal("discard logger contract failed")
	}
}

type assertionError string

func (err assertionError) Error() string { return string(err) }

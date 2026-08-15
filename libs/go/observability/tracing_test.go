// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package observability

import (
	"errors"
	"testing"
)

func TestTraceContextValidationAndAttributes(t *testing.T) {
	trace := TraceContext{
		TraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:  "00f067aa0ba902b7",
		Sampled: true,
	}
	if err := trace.Validate(); err != nil {
		t.Fatal(err)
	}
	fields := trace.Attributes().Fields()
	if fields["trace.id"] != trace.TraceID || fields["span.id"] != trace.SpanID || fields["trace.sampled"] != true {
		t.Fatalf("trace attributes = %#v", fields)
	}
	for _, invalid := range []TraceContext{
		{},
		{TraceID: "00000000000000000000000000000000", SpanID: trace.SpanID},
		{TraceID: trace.TraceID, SpanID: "0000000000000000"},
		{TraceID: "xyz", SpanID: trace.SpanID},
	} {
		if err := invalid.Validate(); !errors.Is(err, ErrInvalidTraceContext) {
			t.Fatalf("Validate(%+v) error = %v", invalid, err)
		}
	}
}

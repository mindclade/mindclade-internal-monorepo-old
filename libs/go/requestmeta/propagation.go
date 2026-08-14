// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package requestmeta

import (
	"context"
	"reflect"
	"strings"
)

const (
	PropagationKeyRequestID     = "mindclade-request-id"
	PropagationKeyCorrelationID = "mindclade-correlation-id"
	PropagationKeyCausationID   = "mindclade-causation-id"
)

// TextMapCarrier is the transport-neutral subset required for metadata
// propagation. HTTP, Connect, gRPC, queue, and workflow adapters can provide
// lightweight wrappers around their native header or metadata types.
type TextMapCarrier interface {
	Get(string) string
	Set(string, string)
}

// MapCarrier is a case-insensitive in-memory carrier useful in tests and
// simple adapters.
type MapCarrier map[string]string

func (carrier MapCarrier) Get(key string) string {
	for candidate, value := range carrier {
		if strings.EqualFold(candidate, key) {
			return value
		}
	}
	return ""
}

func (carrier MapCarrier) Set(key, value string) {
	canonical := strings.ToLower(strings.TrimSpace(key))
	for candidate := range carrier {
		if strings.EqualFold(candidate, canonical) && candidate != canonical {
			delete(carrier, candidate)
		}
	}
	carrier[canonical] = value
}

// Inject writes propagatable metadata from ctx. Logical operation names are
// deliberately local and are not propagated.
func Inject(ctx context.Context, carrier TextMapCarrier) error {
	if ctx == nil {
		return invalidArgument(ErrNilContext, "nil request context", "nil_context", nil)
	}
	if nilTextMapCarrier(carrier) {
		return invalidArgument(ErrNilCarrier, "nil metadata carrier", "nil_carrier", nil)
	}
	metadata, ok := FromContext(ctx)
	if !ok {
		return nil
	}
	if !metadata.RequestID.IsZero() {
		carrier.Set(PropagationKeyRequestID, metadata.RequestID.String())
	}
	if !metadata.CorrelationID.IsZero() {
		carrier.Set(PropagationKeyCorrelationID, metadata.CorrelationID.String())
	}
	if !metadata.CausationID.IsZero() {
		carrier.Set(PropagationKeyCausationID, metadata.CausationID.String())
	}
	return nil
}

// Extract validates metadata from carrier and merges it into ctx. Invalid
// inbound values fail closed with CodeInvalidArgument.
func Extract(ctx context.Context, carrier TextMapCarrier) (context.Context, error) {
	if ctx == nil {
		return nil, invalidArgument(ErrNilContext, "nil request context", "nil_context", nil)
	}
	if nilTextMapCarrier(carrier) {
		return nil, invalidArgument(ErrNilCarrier, "nil metadata carrier", "nil_carrier", nil)
	}

	var metadata Metadata
	if value := strings.TrimSpace(carrier.Get(PropagationKeyRequestID)); value != "" {
		requestID, err := ParseRequestID(value)
		if err != nil {
			return nil, err
		}
		metadata.RequestID = requestID
	}
	if value := strings.TrimSpace(carrier.Get(PropagationKeyCorrelationID)); value != "" {
		correlationID, err := ParseCorrelationID(value)
		if err != nil {
			return nil, err
		}
		metadata.CorrelationID = correlationID
	}
	if value := strings.TrimSpace(carrier.Get(PropagationKeyCausationID)); value != "" {
		causationID, err := ParseCausationID(value)
		if err != nil {
			return nil, err
		}
		metadata.CausationID = causationID
	}
	return WithMetadata(ctx, metadata)
}

// ExtractOrGenerate extracts inbound metadata and guarantees a request ID.
func ExtractOrGenerate(ctx context.Context, carrier TextMapCarrier) (context.Context, RequestID, error) {
	ctx, err := Extract(ctx, carrier)
	if err != nil {
		return nil, RequestID{}, err
	}
	return EnsureRequestID(ctx)
}

func nilTextMapCarrier(carrier TextMapCarrier) bool {
	if carrier == nil {
		return true
	}
	value := reflect.ValueOf(carrier)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

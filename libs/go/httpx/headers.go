// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package httpx

import (
	"net/http"
	"strings"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/requestmeta"
)

const (
	HeaderRequestID      = "Mindclade-Request-Id"
	HeaderCorrelationID  = "Mindclade-Correlation-Id"
	HeaderCausationID    = "Mindclade-Causation-Id"
	HeaderIdempotencyKey = "Idempotency-Key"
	HeaderAPIKey         = "X-Api-Key"
)

// HeaderCarrier adapts HTTP headers to requestmeta.TextMapCarrier.
type HeaderCarrier struct{ Header http.Header }

func (carrier HeaderCarrier) Get(key string) string {
	if carrier.Header == nil {
		return ""
	}
	return carrier.Header.Get(httpHeaderName(key))
}

func (carrier HeaderCarrier) Set(key, value string) {
	if carrier.Header == nil {
		return
	}
	carrier.Header.Set(httpHeaderName(key), value)
}

func httpHeaderName(key string) string {
	switch key {
	case requestmeta.PropagationKeyRequestID:
		return HeaderRequestID
	case requestmeta.PropagationKeyCorrelationID:
		return HeaderCorrelationID
	case requestmeta.PropagationKeyCausationID:
		return HeaderCausationID
	default:
		return key
	}
}

// ValidateRequestMetadataHeaders rejects ambiguous inbound lineage. HTTP
// allows repeated header fields, but request identity must have one canonical
// value so proxies and applications cannot interpret different identifiers.
func ValidateRequestMetadataHeaders(header http.Header) error {
	for _, name := range []string{HeaderRequestID, HeaderCorrelationID, HeaderCausationID} {
		values := header.Values(name)
		nonEmpty := 0
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				nonEmpty++
			}
		}
		if nonEmpty > 1 {
			return faults.New(
				faults.CodeInvalidArgument,
				"ambiguous request metadata",
				faults.WithReason("ambiguous_request_metadata"),
				faults.WithOperation("httpx.ValidateRequestMetadataHeaders"),
				faults.WithField("header", name),
			)
		}
	}
	return nil
}

// InjectRequestMetadata writes request lineage from ctx into request headers.
func InjectRequestMetadata(request *http.Request) error {
	if request == nil {
		return ErrInvalidResponse
	}
	return requestmeta.Inject(request.Context(), HeaderCarrier{Header: request.Header})
}

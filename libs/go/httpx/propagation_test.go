// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package httpx

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"mindclade.internal/libs/go/requestmeta"
)

func TestPrepareRequestGeneratesLineageWithoutMutatingCaller(t *testing.T) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test/runs", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Test", "original")

	prepared, err := PrepareRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if prepared == request {
		t.Fatal("request was not cloned")
	}
	if prepared.Header.Get(HeaderRequestID) == "" {
		t.Fatal("request identifier was not propagated")
	}
	if request.Header.Get(HeaderRequestID) != "" {
		t.Fatal("caller request was mutated")
	}
	if _, ok := requestmeta.RequestIDFromContext(prepared.Context()); !ok {
		t.Fatal("prepared context has no request identifier")
	}
	prepared.Header.Set("X-Test", "changed")
	if request.Header.Get("X-Test") != "original" {
		t.Fatal("header map was not cloned")
	}
}

func TestRequestMetadataTransportDelegatesPreparedRequest(t *testing.T) {
	var captured *http.Request
	transport := RequestMetadataTransport{Base: RoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		captured = request
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    request,
		}, nil
	})}
	request, err := http.NewRequest(http.MethodGet, "https://example.test/runs", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if captured == nil || captured.Header.Get(HeaderRequestID) == "" {
		t.Fatal("prepared request was not delegated")
	}
}

func TestPrepareRequestRejectsNil(t *testing.T) {
	if _, err := PrepareRequest(nil); err == nil {
		t.Fatal("expected nil request error")
	}
}

func TestValidateRequestMetadataHeadersRejectsAmbiguity(t *testing.T) {
	header := http.Header{}
	header.Add(HeaderRequestID, "request_019c7af21b8276d2a0d522fe41739a21")
	header.Add(HeaderRequestID, "request_019c7af21b827f53a6b84710f1815c84")
	if err := ValidateRequestMetadataHeaders(header); err == nil {
		t.Fatal("expected ambiguous lineage error")
	}
}

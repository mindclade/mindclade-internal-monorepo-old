// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package otel

import (
	"net/http"
	"testing"

	"mindclade.internal/libs/go/faults"
)

type nilHandler struct{}

func (*nilHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

type nilTransport struct{}

func (*nilTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, nil }

func TestNewHandler(t *testing.T) {
	handler, err := NewHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), "http.test")
	if err != nil || handler == nil {
		t.Fatalf("handler=%v err=%v", handler, err)
	}
	if _, err := NewHandler(nil, "http.test"); faults.CodeOf(err) != faults.CodeInvalidArgument {
		t.Fatalf("nil handler code=%s err=%v", faults.CodeOf(err), err)
	}
	var typedNil *nilHandler
	if _, err := NewHandler(typedNil, "http.test"); faults.CodeOf(err) != faults.CodeInvalidArgument {
		t.Fatalf("typed nil handler code=%s err=%v", faults.CodeOf(err), err)
	}
	if _, err := NewHandler(http.NotFoundHandler(), ""); faults.CodeOf(err) != faults.CodeInvalidArgument {
		t.Fatalf("empty operation code=%s err=%v", faults.CodeOf(err), err)
	}
}

func TestNewTransportDefaultsTypedNil(t *testing.T) {
	var transport *nilTransport
	if wrapped := NewTransport(transport); wrapped == nil {
		t.Fatal("expected transport")
	}
}

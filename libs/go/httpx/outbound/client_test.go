// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package outbound

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

type staticResolver map[string][]netip.Addr

func (resolver staticResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return append([]netip.Addr(nil), resolver[host]...), nil
}

func TestRejectsPrivateResolution(t *testing.T) {
	client, err := NewClientWithResolver(Policy{AllowedHosts: []string{"example.test"}}, staticResolver{"example.test": {netip.MustParseAddr("127.0.0.1")}})
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if _, err := client.Do(request); err == nil {
		t.Fatal("expected private address rejection")
	}
}

func TestAllowsExplicitPrivateTestEndpointAndBoundsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(strings.Repeat("x", 32)))
	}))
	defer server.Close()
	host, _, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	client, err := NewClient(Policy{
		AllowedHosts: []string{host}, AllowHTTPForTests: true, AllowPrivateAddresses: true,
		AllowedMediaTypes: []string{"application/json"}, MaxResponseBytes: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	response, err := client.Do(request)
	if err != nil {
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatal(err)
		}
		return
	}
	defer response.Body.Close()
	if _, err := io.ReadAll(response.Body); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("expected body limit error, got %v", err)
	}
}

func TestRedirectIsRevalidated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "http://blocked.test/path", http.StatusFound)
	}))
	defer server.Close()
	host, _, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	client, err := NewClient(Policy{AllowedHosts: []string{host}, AllowHTTPForTests: true, AllowPrivateAddresses: true})
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	if _, err := client.Do(request); err == nil {
		t.Fatal("expected redirect rejection")
	}
}

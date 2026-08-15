// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
// End-to-end tests through the assembled server.
//
// The packages below each have their own tests; these check that they are WIRED
// correctly, which is a different failure. A perfectly correct IAP verifier
// mounted behind the wrong middleware, or probes registered inside the auth
// chain, are both invisible to a unit test and fatal in a cluster.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mindclade.internal/services/studio/internal/httpx"
	"mindclade.internal/services/studio/internal/iap"
	"mindclade.internal/services/studio/internal/session"
)

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func testCodec(t *testing.T) *session.Codec {
	t.Helper()
	material := make([]byte, 32)
	for i := range material {
		material[i] = 0x42
	}
	key, err := session.NewKey("k1", material)
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	c, err := session.NewCodec(key, nil, 1)
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}
	return c
}

func testVerifier(t *testing.T) *iap.Verifier {
	t.Helper()
	v, err := iap.NewVerifier("/projects/1/global/backendServices/1", nil)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func buildRole(t *testing.T, role Role) http.Handler {
	t.Helper()
	d := Deps{Role: role, Logger: discardLogger(), CSPMode: httpx.CSPReportOnly}
	if role != RoleEmbed {
		d.Verifier = testVerifier(t)
		d.Codec = testCodec(t)
		d.Resolve = func(context.Context, iap.Assertion) error { return nil }
	}
	h, err := Build(d)
	if err != nil {
		t.Fatalf("Build(%s): %v", role, err)
	}
	return h
}

// A readiness probe carries no IAP assertion, because the kubelet is not a
// browser. Registered inside the auth chain, every probe 401s, the Deployment
// never becomes ready, and the pod logs show only 401s with nothing naming the
// probe.
func TestProbesBypassAuthentication(t *testing.T) {
	for _, role := range []Role{RoleWeb, RoleEmbed} {
		h := buildRole(t, role)
		for _, path := range []string{"/healthz", "/readyz"} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Errorf("%s %s: status = %d, want 200", role, path, rec.Code)
			}
		}
	}
}

// The acceptance check, in Go: a forged assertion sent straight at the pod —
// bypassing the load balancer entirely — must be 401 and never 200.
func TestForgedAssertionAtThePodIsRejected(t *testing.T) {
	h := buildRole(t, RoleWeb)

	req := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	req.Header.Set(iap.HeaderName, "not.a.jwt")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// No assertion at all is equally rejected. Reaching a pod directly must not be
// a way around IAP.
func TestNoAssertionIsRejected(t *testing.T) {
	h := buildRole(t, RoleWeb)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// A stolen session cookie with no accompanying assertion is inert. This is the
// property that makes holding the session client-side safe at all.
func TestSessionCookieAloneIsInert(t *testing.T) {
	codec := testCodec(t)
	value, err := codec.Seal("accounts.google.com:1", "sess-1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	h := buildRole(t, RoleWeb)
	req := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: value})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — a cookie without an assertion must not authenticate", rec.Code)
	}
}

// The embed role is reachable WITHOUT any session, deliberately. If this ever
// starts redirecting, IAP has been attached to it and the docs widget is broken
// in every browser that partitions storage.
func TestEmbedNeedsNoSession(t *testing.T) {
	h := buildRole(t, RoleEmbed)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embed/demo", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("embed redirected to %q; IAP must not be in front of it", loc)
	}
}

// The embed surface must never issue a cookie, and must be framable by docs
// alone.
func TestEmbedIsCookielessAndFramableByDocsOnly(t *testing.T) {
	h := buildRole(t, RoleEmbed)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embed/demo", nil))

	if got := rec.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("embed set a cookie: %v", got)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors https://docs.mindclade.dev") {
		t.Errorf("CSP = %q; want frame-ancestors scoped to docs", csp)
	}
}

// The echo endpoint exists for the acceptance check that the Cookie header is
// stripped AT THE ROUTE. If a cookie reaches here, the route filter is gone.
func TestEmbedEchoRevealsWhetherCookiesArrive(t *testing.T) {
	h := buildRole(t, RoleEmbed)

	req := httptest.NewRequest(http.MethodGet, "/embed/_echo_headers", nil)
	req.Header.Set("Cookie", "__Host-mc_session=whatever")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The handler echoes faithfully; the STRIP is the route's job. This test
	// pins the diagnostic, so the shell check against a live cluster is
	// meaningful.
	if got["cookie"] != "__Host-mc_session=whatever" {
		t.Errorf("echo did not report the header it received: %v", got)
	}
}

// Build must refuse rather than serve an authenticated surface with no
// authentication. A nil verifier would not fail until the first request, by
// which point the Service is already behind a route.
func TestBuildRefusesIncompleteAuthenticatedRoles(t *testing.T) {
	for _, role := range []Role{RoleWeb, RoleBFF, RoleBFFStream} {
		if _, err := Build(Deps{Role: role, Logger: discardLogger()}); err == nil {
			t.Errorf("Build(%s) with no verifier succeeded; want an error", role)
		}
	}
}

// The roles that touch the run log must not start without one.
func TestBuildRequiresADatabaseWhereItIsUsed(t *testing.T) {
	for _, role := range []Role{RoleBFF, RoleBFFStream} {
		d := Deps{
			Role: role, Logger: discardLogger(),
			Verifier: testVerifier(t), Codec: testCodec(t),
			Resolve: func(context.Context, iap.Assertion) error { return nil },
		}
		if _, err := Build(d); err == nil {
			t.Errorf("Build(%s) with no database succeeded; want an error", role)
		}
	}
}

func TestParseRole(t *testing.T) {
	for _, s := range []string{"web", "bff", "bff-stream", "embed"} {
		if _, err := ParseRole(s); err != nil {
			t.Errorf("ParseRole(%q): %v", s, err)
		}
	}
	for _, s := range []string{"", "WEB", "stream", "api"} {
		if _, err := ParseRole(s); err == nil {
			t.Errorf("ParseRole(%q) succeeded; want an error", s)
		}
	}
}

// Security headers must be present on the authenticated roles even when the
// request is rejected — a 401 is still a response a browser renders.
func TestSecurityHeadersApplyToRejectedRequests(t *testing.T) {
	h := buildRole(t, RoleWeb)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get("Content-Security-Policy-Report-Only") == "" {
		t.Error("no CSP on a 401 response")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("no nosniff on a 401 response")
	}
}

func TestDigestDistinguishesBodies(t *testing.T) {
	if digest([]byte(`{"a":1}`)) == digest([]byte(`{"a":2}`)) {
		t.Fatal("different bodies produced the same digest; a replayed key would not be detected")
	}
	if digest([]byte(`{"a":1}`)) != digest([]byte(`{"a":1}`)) {
		t.Fatal("identical bodies produced different digests; a legitimate retry would 409")
	}
}

func TestNewUUIDIsWellFormedAndUnique(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := newUUID()
		if len(id) != 36 || strings.Count(id, "-") != 4 {
			t.Fatalf("malformed uuid %q", id)
		}
		if id[14] != '4' {
			t.Fatalf("uuid %q is not version 4", id)
		}
		if seen[id] {
			t.Fatalf("duplicate uuid %q", id)
		}
		seen[id] = true
	}
}

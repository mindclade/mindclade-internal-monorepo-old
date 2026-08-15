// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func ok(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func TestCSRFAllowsSafeMethods(t *testing.T) {
	h := CSRF(http.HandlerFunc(ok))
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/api/canvas/1", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", m, rec.Code)
		}
	}
}

// PATCH is the method the canvas uses. A check written as "not POST" would let
// it through, which is a CSRF hole on the one endpoint that mutates documents.
func TestCSRFBlocksEveryStateChangingMethodWithoutProof(t *testing.T) {
	h := CSRF(http.HandlerFunc(ok))
	for _, m := range []string{http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/api/canvas/1", nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s with no Origin: status = %d, want 403", m, rec.Code)
		}
	}
}

func TestCSRFAcceptsMatchingOrigin(t *testing.T) {
	h := CSRF(http.HandlerFunc(ok))
	req := httptest.NewRequest(http.MethodPost, "/api/runs", nil)
	req.Header.Set("Origin", Origin)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestCSRFRejectsForeignOrigins(t *testing.T) {
	h := CSRF(http.HandlerFunc(ok))
	for _, origin := range []string{
		"https://evil.example",
		"http://mindclade.studio",       // wrong scheme
		"https://mindclade.studio.evil", // suffix attack
		"https://api.mindclade.ai",      // our own machine plane is still cross-origin
		"null",
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/runs", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("Origin %q: status = %d, want 403", origin, rec.Code)
		}
	}
}

func TestCSRFAcceptsSecFetchSameOrigin(t *testing.T) {
	h := CSRF(http.HandlerFunc(ok))
	req := httptest.NewRequest(http.MethodPost, "/api/runs", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// Partial Sec-Fetch evidence is not evidence. A cross-site navigation carries
// Sec-Fetch-Site alone, and accepting that would defeat the check.
func TestCSRFRejectsPartialSecFetch(t *testing.T) {
	h := CSRF(http.HandlerFunc(ok))
	for _, hdrs := range []map[string]string{
		{"Sec-Fetch-Site": "same-origin"},
		{"Sec-Fetch-Mode": "cors"},
		{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "cors"},
		{"Sec-Fetch-Site": "same-site", "Sec-Fetch-Mode": "cors"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/runs", nil)
		for k, v := range hdrs {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%v: status = %d, want 403", hdrs, rec.Code)
		}
	}
}

func TestCSPIsReportOnlyByDefault(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(CSPReportOnly, "https://csp.example/report", http.HandlerFunc(ok)).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get("Content-Security-Policy-Report-Only") == "" {
		t.Error("no Report-Only header")
	}
	if rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("enforcing header set in report-only mode")
	}
	if !strings.Contains(rec.Header().Get("Report-To"), "csp") {
		t.Error("Report-To missing; a Report-Only policy with nowhere to report collects nothing")
	}
}

// The architecture policing itself: if the machine plane's hostname ever
// appears in this policy, a browser call to it has been allowed.
func TestCSPNeverNamesTheMachinePlane(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(CSPEnforce, "", http.HandlerFunc(ok)).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	policy := rec.Header().Get("Content-Security-Policy")
	if strings.Contains(policy, "mindclade.ai") {
		t.Fatalf("policy names the machine plane:\n%s", policy)
	}
	for _, want := range []string{
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"object-src 'none'",
		"require-trusted-types-for 'script'",
		"'strict-dynamic'",
	} {
		if !strings.Contains(policy, want) {
			t.Errorf("policy missing %q", want)
		}
	}
}

// A repeated nonce is as good as no nonce.
func TestNoncesAreFreshPerResponse(t *testing.T) {
	h := SecurityHeaders(CSPEnforce, "", http.HandlerFunc(ok))
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		policy := rec.Header().Get("Content-Security-Policy")
		if seen[policy] {
			t.Fatal("nonce repeated across responses")
		}
		seen[policy] = true
	}
}

// The nonce must reach the handler, or the shell cannot put it on script tags.
func TestNonceIsAvailableToTheHandler(t *testing.T) {
	var got string
	SecurityHeaders(CSPEnforce, "", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = Nonce(r)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got == "" {
		t.Fatal("no nonce in the request context")
	}
}

// A cached shell carries a stale nonce and every script on the page then fails
// the policy — intermittently, which is worse than always.
func TestShellIsNotCacheable(t *testing.T) {
	rec := httptest.NewRecorder()
	NoStoreShell(http.HandlerFunc(ok)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

// HSTS must not advertise preload: submission requires public reachability,
// which these hostnames will never have, and it is painful to walk back.
func TestHSTSDoesNotClaimPreload(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(CSPEnforce, "", http.HandlerFunc(ok)).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	hsts := rec.Header().Get("Strict-Transport-Security")
	if !strings.Contains(hsts, "max-age=63072000") {
		t.Errorf("HSTS = %q", hsts)
	}
	if strings.Contains(hsts, "preload") {
		t.Errorf("HSTS advertises preload, which cannot be fulfilled: %q", hsts)
	}
}

// Referrer-Policy: same-origin keeps /o/<id> and /c/<docId> out of other
// people's logs.
func TestReferrerPolicyIsSameOrigin(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(CSPEnforce, "", http.HandlerFunc(ok)).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Referrer-Policy"); got != "same-origin" {
		t.Errorf("Referrer-Policy = %q", got)
	}
}

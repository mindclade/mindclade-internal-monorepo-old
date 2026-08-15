// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package httpx carries the browser plane's request-path controls: the
// IAP-plus-session handshake, the CSRF check, and the security headers.
package httpx

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

// Origin is the single browser origin. Everything the SPA fetches is a
// same-origin path under it.
//
// One origin is what makes the CSRF check below TOTAL rather than a list to
// maintain: there is no legitimate cross-origin caller, ever, so there is no
// allowlist and nothing to keep in step.
const Origin = "https://mindclade.studio"

// CSRF rejects state-changing requests that did not come from this origin.
//
// SameSite=Lax on the session cookie already covers the classic cross-site form
// post. This closes what Lax does not: a same-site but cross-ORIGIN request,
// and any browser that has not implemented Lax defaults.
//
// Two accepted signals, either sufficient:
//
//	Origin: https://mindclade.studio
//	Sec-Fetch-Site: same-origin  AND  Sec-Fetch-Mode: cors
//
// Sec-Fetch-* is the stronger signal because the browser sets it and script
// cannot forge it, but it is absent on older browsers — hence both.
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if safeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get("Origin") == Origin {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get("Sec-Fetch-Site") == "same-origin" &&
			r.Header.Get("Sec-Fetch-Mode") == "cors" {
			next.ServeHTTP(w, r)
			return
		}

		// No detail in the response. A cross-origin caller learns only that it
		// was refused, not which check refused it.
		http.Error(w, "forbidden", http.StatusForbidden)
	})
}

// safeMethod reports whether a method is non-state-changing.
//
// The list is deliberately exhaustive rather than "not POST": a PATCH or DELETE
// slipping through an inverted check is a CSRF hole, and PATCH is the method
// the canvas uses.
func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// CSPMode selects enforcement or report-only.
type CSPMode int

const (
	// CSPReportOnly collects violations without blocking. Ship in this mode
	// FIRST, with a working report endpoint — a Report-Only policy with nowhere
	// to report is theatre.
	//
	// Collect for one full usage cycle including a structure-viewer session and
	// a long streaming run, triage the Trusted Types violations separately
	// since they will dominate the volume, then enforce.
	CSPReportOnly CSPMode = iota

	CSPEnforce
)

// SecurityHeaders sets the browser plane's response headers.
//
// # connect-src 'self' makes the architecture police itself
//
// A single origin means the policy can be `connect-src 'self'`, so if anyone
// adds a direct browser call to api.mindclade.ai it fails immediately in
// development rather than shipping. The machine plane's hostname must never
// appear in this policy.
//
// # Three deliberate choices
//
// style-src takes 'unsafe-inline' rather than a nonce. A nonce on style-src
// breaks runtime <style> injection, which most CSS-in-JS and several component
// libraries do — and it is the single most common reason a CSP rollout gets
// reverted wholesale. Reverting the whole policy to save the style directive is
// a bad trade. The weakening is real but bounded: with object-src 'none',
// base-uri 'none', nonced scripts and connect-src 'self', injected CSS cannot
// execute or exfiltrate to another origin.
//
// 'strict-dynamic' stays because route-based code splitting emits dynamic
// import(), and those loads fail under a bare nonce policy. Drop it only after
// confirming the BUILD OUTPUT has no dynamic import — bundlers introduce them
// for route chunks whether or not anyone wrote one.
//
// require-trusted-types-for 'script' is the point of the whole policy. The
// browser holds no bearer token, so the realistic XSS payload here is not theft
// but action-on-behalf, and Trusted Types closes the DOM-XSS sinks a nonce
// policy leaves wide open. Expect it to produce the most Report-Only violations
// and at least one in a third-party library.
func SecurityHeaders(mode CSPMode, reportURI string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce, err := newNonce()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		policy := strings.Join([]string{
			"default-src 'none'",
			fmt.Sprintf("script-src 'nonce-%s' 'strict-dynamic'", nonce),
			"connect-src 'self'",
			"img-src 'self' blob: data:",
			"style-src 'self' 'unsafe-inline'",
			"font-src 'self' data:",
			"worker-src 'self' blob:",
			"frame-ancestors 'none'",
			"base-uri 'none'",
			"object-src 'none'",
			"form-action 'self'",
			"require-trusted-types-for 'script'",
			"trusted-types default",
			"report-to csp",
		}, "; ")

		header := "Content-Security-Policy-Report-Only"
		if mode == CSPEnforce {
			header = "Content-Security-Policy"
		}
		w.Header().Set(header, policy)

		if reportURI != "" {
			// Without this the Report-Only phase collects nothing, which is the
			// most common way a CSP rollout produces false confidence.
			w.Header().Set("Report-To", fmt.Sprintf(
				`{"group":"csp","max_age":10886400,"endpoints":[{"url":"%s"}]}`, reportURI))
		}

		// same-origin, so /c/<docId> and /o/<id> paths never leak to docs. A
		// handoff id in a Referer header is a capability in someone else's logs.
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")

		// Two years, and deliberately NO `preload` token. Submission to the
		// preload list requires the site to be publicly reachable for
		// verification, which by design it never will be — advertising an
		// intent that cannot be fulfilled is painful to walk back.
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")

		next.ServeHTTP(w, WithNonce(r, nonce))
	})
}

// NoStoreShell marks the HTML shell uncacheable.
//
// # This is not a performance footnote
//
// A per-response nonce dictates the cache policy. A cached shell carries a
// STALE nonce, and every script on the page then fails the policy — which
// breaks INTERMITTENTLY, depending on whether a given user got a cached copy.
// That is the most common way a nonce-based CSP fails in production, and
// intermittent is worse than total.
//
// Content-hashed JS and CSS bundles are unaffected and stay immutable with a
// long max-age, because the nonce lives only in the shell.
func NoStoreShell(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func newNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

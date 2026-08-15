// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package vanity

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	repo = "https://github.com/mindclade-org/mindclade"
	docs = "https://docs.mindclade.dev/go"
)

func newTestHandler(t *testing.T, rules ...Rule) *Handler {
	t.Helper()
	if len(rules) == 0 {
		rules = []Rule{{Prefix: "go.mindclade.dev", VCS: "git", RepoURL: repo}}
	}
	h, err := New(docs, rules...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func get(t *testing.T, h *Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = "go.mindclade.dev"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The two cases the acceptance checks call out by name: a deep subpath, and the
// bare root. The root is the one a handler ported from a path-prefixed layout
// would 404, because there the root was free to.
func TestServesDeepPathAndBareRoot(t *testing.T) {
	h := newTestHandler(t)

	for _, target := range []string{
		"/?go-get=1",
		"/serving/gateway?go-get=1",
		"/a/b/c/d/e?go-get=1",
		"/?go-get=1&other=ignored",
	} {
		rec := get(t, h, target)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", target, rec.Code)
		}
		want := `content="go.mindclade.dev git ` + repo + `"`
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: body missing %s\ngot: %s", target, want, rec.Body.String())
		}
	}
}

// A trailing slash is the same module. Left unhandled it produces a module path
// ending in "/" that matches no rule, and the go command reports that the
// package does not exist.
func TestTrailingSlashIsTheSameModule(t *testing.T) {
	h := newTestHandler(t)
	if rec := get(t, h, "/serving/gateway/?go-get=1"); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// Without ?go-get=1 this host has nothing to say to a browser.
func TestNonGoGetRedirectsToDocs(t *testing.T) {
	h := newTestHandler(t)
	rec := get(t, h, "/serving/gateway")
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != docs {
		t.Errorf("Location = %q, want %q", got, docs)
	}
}

// Longest-prefix-first. Matching the general rule instead would send the go
// command to the wrong repository, where the failure reads "package does not
// exist" and never "asked the wrong repository".
func TestMostSpecificRuleWins(t *testing.T) {
	h := newTestHandler(t,
		Rule{Prefix: "go.mindclade.dev", VCS: "git", RepoURL: repo},
		Rule{Prefix: "go.mindclade.dev/tools", VCS: "git", RepoURL: "https://github.com/mindclade-org/tools"},
	)

	for _, tc := range []struct{ path, wantRepo string }{
		{"/tools?go-get=1", "https://github.com/mindclade-org/tools"},
		{"/tools/lint?go-get=1", "https://github.com/mindclade-org/tools"},
		{"/serving/gateway?go-get=1", repo},
		{"/?go-get=1", repo},
		// The boundary case a naive strings.HasPrefix gets wrong: a DIFFERENT
		// module whose name merely starts with "tools".
		{"/toolsmith?go-get=1", repo},
	} {
		rec := get(t, h, tc.path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", tc.path, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), tc.wantRepo+`"`) {
			t.Errorf("%s: wrong repo\ngot: %s\nwant: %s", tc.path, rec.Body.String(), tc.wantRepo)
		}
	}
}

// Rules are supplied in the order a human would WRITE them — general first —
// and must still be evaluated most-specific-first.
func TestRuleOrderIndependence(t *testing.T) {
	specific := Rule{Prefix: "go.mindclade.dev/tools", VCS: "git", RepoURL: "https://github.com/mindclade-org/tools"}
	general := Rule{Prefix: "go.mindclade.dev", VCS: "git", RepoURL: repo}

	for _, order := range [][]Rule{{general, specific}, {specific, general}} {
		h := newTestHandler(t, order...)
		rec := get(t, h, "/tools/lint?go-get=1")
		if !strings.Contains(rec.Body.String(), "mindclade-org/tools") {
			t.Errorf("order %v: matched the general rule; body: %s", order, rec.Body.String())
		}
	}
}

// A host this handler does not serve gets a 404 rather than a tag naming a
// repository it has no business advertising.
func TestUnknownHostIsNotFound(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/serving/gateway?go-get=1", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// Reached through a port-forward, r.Host carries a port. Stripping it is what
// keeps `kubectl port-forward` debugging from 404ing confusingly.
func TestHostPortIsIgnored(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/serving/gateway?go-get=1", nil)
	req.Host = "go.mindclade.dev:8080"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// A cached tag pointing at a moved repository is a resolution failure nobody
// can reproduce.
func TestResponseIsNotCacheable(t *testing.T) {
	h := newTestHandler(t)
	rec := get(t, h, "/?go-get=1")
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestRejectsEmptyAndDuplicateRules(t *testing.T) {
	if _, err := New(docs); err == nil {
		t.Error("New with no rules: want error, got nil")
	}
	if _, err := New(docs, Rule{Prefix: "go.mindclade.dev", VCS: "git"}); err == nil {
		t.Error("New with empty RepoURL: want error, got nil")
	}
	dup := Rule{Prefix: "go.mindclade.dev", VCS: "git", RepoURL: repo}
	if _, err := New(docs, dup, dup); err == nil {
		t.Error("New with duplicate prefixes: want error, got nil")
	}
}

func TestRejectsNonGetMethods(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/?go-get=1", nil)
	req.Host = "go.mindclade.dev"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

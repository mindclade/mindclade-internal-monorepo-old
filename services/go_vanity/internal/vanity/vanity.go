// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package vanity serves the go-import meta tags that make go.mindclade.dev a
// resolvable Go module path.
//
// # What the go command actually does
//
// When `go get go.mindclade.dev/serving/gateway` runs, the toolchain issues
//
//	GET https://go.mindclade.dev/serving/gateway?go-get=1
//
// and parses the HTML for a <meta name="go-import"> tag. The tag names three
// space-separated fields: the module path prefix, the VCS, and the repository
// URL. The go command then clones that repository and looks for the package at
// the remainder of the path.
//
// # Two requirements that are easy to miss
//
// The handler must answer EVERY subpath, not just known ones. It cannot know
// which packages exist — that is the repository's business, not the vanity
// endpoint's — so it answers uniformly and lets the clone decide.
//
// It must also answer the BARE ROOT. Under a bare-hostname module path,
// `GET /?go-get=1` is the module-root request. A handler ported from a layout
// with a path prefix (`go.mindclade.dev/mono/...`) would have been free to 404
// the root, and that is the one case such a port is guaranteed to get wrong.
//
// # Why this is unauthenticated
//
// The go command sends NO credentials unless ~/.netrc has an entry for the
// host. Behind an auth redirect it receives an HTML login page and fails with a
// meta-tag parse error naming neither auth nor the redirect — a genuinely
// baffling failure. What is disclosed here is one repository URL, to a caller
// already inside the perimeter; the alternative is provisioning .netrc on every
// developer machine and CI runner forever. See the route manifest.
package vanity

import (
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
)

// Rule maps a module path prefix to the repository that holds it.
type Rule struct {
	// Prefix is the module path, without a scheme. For the module at the root
	// of the namespace this is the bare hostname: "go.mindclade.dev".
	Prefix string

	// VCS is the version control system. "git" in every case here; the field
	// exists because the meta tag has a slot for it, not because it varies.
	VCS string

	// RepoURL is what the go command clones. HTTPS rather than ssh:// so that
	// resolution works without a key on the calling machine — Athens supplies
	// its own credentials, and no other client resolves this host directly.
	RepoURL string
}

// Handler serves go-import meta tags for a set of rules.
type Handler struct {
	// rules, sorted MOST SPECIFIC FIRST. Order is the whole correctness
	// argument for multi-module support: with rules for both
	// "go.mindclade.dev" and "go.mindclade.dev/tools", a request for
	// /tools/foo must match the latter. Matching the former would send the go
	// command to the wrong repository, where it would fail to find the package
	// and report that the package does not exist — never that it asked the
	// wrong repository.
	rules []Rule

	// docsURL receives anything that is not a ?go-get=1 request.
	docsURL string
}

// New returns a Handler serving the given rules.
//
// Rules are sorted internally, so callers need not supply them in any
// particular order — which matters because the natural order to WRITE them
// (general first) is the opposite of the order they must be EVALUATED in.
func New(docsURL string, rules ...Rule) (*Handler, error) {
	if len(rules) == 0 {
		return nil, fmt.Errorf("vanity: no rules configured; the handler would 404 every module")
	}

	seen := make(map[string]struct{}, len(rules))
	for _, r := range rules {
		if r.Prefix == "" || r.VCS == "" || r.RepoURL == "" {
			return nil, fmt.Errorf("vanity: rule %+v has an empty field; the meta tag would be malformed and the go command would report a parse error", r)
		}
		if _, dup := seen[r.Prefix]; dup {
			return nil, fmt.Errorf("vanity: duplicate prefix %q; which repository wins would depend on sort stability", r.Prefix)
		}
		seen[r.Prefix] = struct{}{}
	}

	sorted := make([]Rule, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].Prefix) > len(sorted[j].Prefix)
	})

	return &Handler{rules: sorted, docsURL: docsURL}, nil
}

var metaTemplate = template.Must(template.New("go-import").Parse(
	`<!DOCTYPE html>
<html>
<head>
<meta name="go-import" content="{{.Prefix}} {{.VCS}} {{.RepoURL}}">
<meta name="viewport" content="width=device-width, initial-scale=1">
</head>
<body>
Module <code>{{.Prefix}}</code> is served from <code>{{.RepoURL}}</code>.
</body>
</html>
`))

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only GET and HEAD. The go command issues GET; anything else on this host
	// is not the toolchain.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Everything that is not a module resolution goes to the docs. A human who
	// pastes go.mindclade.dev into a browser should land somewhere useful
	// rather than on a meta tag.
	if r.URL.Query().Get("go-get") != "1" {
		if h.docsURL != "" {
			http.Redirect(w, r, h.docsURL, http.StatusFound)
			return
		}
		http.NotFound(w, r)
		return
	}

	// The module path is the host plus the request path. Host, not a hardcoded
	// constant: the same binary serves whatever name reaches it, and hardcoding
	// would break the moment this is exercised through a port-forward.
	//
	// r.Host may carry a port when reached directly rather than through the
	// load balancer.
	host := r.Host
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	modulePath := host + strings.TrimSuffix(r.URL.Path, "/")

	rule, ok := h.match(modulePath)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// no-store rather than a cache lifetime. These responses are tiny and
	// resolution is rare — Athens is the only client — so there is nothing to
	// gain, and a cached tag pointing at a moved repository is a resolution
	// failure nobody can reproduce.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	if r.Method == http.MethodHead {
		return
	}
	_ = metaTemplate.Execute(w, rule)
}

// match returns the most specific rule whose prefix covers modulePath.
//
// A rule covers a path if the path IS the prefix, or begins with the prefix
// followed by "/". The second condition is load-bearing: without it a rule for
// "go.mindclade.dev/tools" would also match "go.mindclade.dev/toolsmith",
// which is a different module served the wrong repository.
func (h *Handler) match(modulePath string) (Rule, bool) {
	for _, r := range h.rules {
		if modulePath == r.Prefix || strings.HasPrefix(modulePath, r.Prefix+"/") {
			return r, true
		}
	}
	return Rule{}, false
}

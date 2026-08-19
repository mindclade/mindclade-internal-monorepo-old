// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
// Package server assembles the browser plane's four roles.
//
// # One binary, four Services
//
// The four roles ship as one image and four Deployments, not four codebases.
// The split that matters is at the SERVICE level, because IAP and backend
// timeouts attach per (Service, port) and two paths cannot differ in either
// unless they route to different Services. Nothing about that requires four
// binaries, and four binaries would mean four places to fix the next session
// bug.
//
// So the Service boundary is the enforcement mechanism and the role flag is
// merely how one image knows which mux to serve.
//
//	web           SPA shell. IAP on. Also serves IAP's /_gcp_iap/* callback via
//	              the catch-all route, which is why removing IAP from THIS role
//	              specifically breaks sign-in estate-wide.
//	bff           JSON API and handoff redemption. IAP on.
//	bff-stream    SSE only. IAP on, and the one role carrying a 900s backend
//	              timeout.
//	embed         Cookieless, sessionless, NO IAP. The Cookie header is stripped
//	              at the route before it arrives.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"go.mindclade.dev/libs/go/httpx/health"

	"go.mindclade.dev/services/studio/internal/handoff"
	"go.mindclade.dev/services/studio/internal/httpx"
	"go.mindclade.dev/services/studio/internal/iap"
	"go.mindclade.dev/services/studio/internal/runlog"
	"go.mindclade.dev/services/studio/internal/session"
	"go.mindclade.dev/services/studio/internal/stream"
)

// Role selects which mux this process serves.
type Role string

const (
	RoleWeb       Role = "web"
	RoleBFF       Role = "bff"
	RoleBFFStream Role = "bff-stream"
	RoleEmbed     Role = "embed"
)

// ParseRole validates a role name.
func ParseRole(s string) (Role, error) {
	switch Role(s) {
	case RoleWeb, RoleBFF, RoleBFFStream, RoleEmbed:
		return Role(s), nil
	default:
		return "", fmt.Errorf("server: unknown role %q; want web, bff, bff-stream, or embed", s)
	}
}

// Deps is everything a role might need. Which fields are required depends on
// the role — Build says so explicitly rather than nil-panicking at the first
// request.
type Deps struct {
	Role   Role
	Logger *slog.Logger

	// Nil for the embed role, which has no session and no IAP by design.
	Verifier *iap.Verifier
	Codec    *session.Codec
	Resolve  httpx.Resolver

	DB *sql.DB

	// Health answers the probes. Required for every role: an unset one would
	// leave the probe routes reporting a fixed 200, which is the defect this
	// field exists to prevent.
	Health *Health

	// CSP posture. Ship Report-Only first, with a working endpoint.
	CSPMode      httpx.CSPMode
	CSPReportURI string
}

// Build returns the handler for a role.
func Build(d Deps) (http.Handler, error) {
	if d.Logger == nil {
		return nil, errors.New("server: logger is required")
	}
	if d.Health == nil {
		return nil, errors.New("server: health is required")
	}

	switch d.Role {
	case RoleEmbed:
		return buildEmbed(d), nil
	case RoleWeb, RoleBFF, RoleBFFStream:
		if d.Verifier == nil || d.Codec == nil || d.Resolve == nil {
			// Refuse to start rather than serve an authenticated surface with
			// no authentication. A nil verifier here would not fail until the
			// first request, by which point the Service is behind a route.
			return nil, fmt.Errorf("server: role %q requires an IAP verifier, a session codec, and a resolver", d.Role)
		}
		if d.Role != RoleWeb && d.DB == nil {
			return nil, fmt.Errorf("server: role %q requires a database", d.Role)
		}
		return buildAuthenticated(d)
	default:
		return nil, fmt.Errorf("server: unhandled role %q", d.Role)
	}
}

// buildEmbed serves the read-only, cookieless surface.
//
// # What may be served here is capped by what protects it
//
// This role has NO IAP, because IAP's sign-in redirect cannot complete inside
// an iframe and an existing IAP cookie is third-party from docs' perspective.
// Its only protection is network reachability.
//
// Therefore it may only render content already readable by anyone with network
// access. If a widget ever needs authorization, it cannot be an iframe — make it
// a link to /c/<docId> instead.
//
// It never reads a cookie. The route strips the Cookie header before it arrives,
// and this code ignores cookies regardless, because a route filter is one edit
// from being removed.
func buildEmbed(d Deps) http.Handler {
	mux := http.NewServeMux()
	mountProbes(mux, d)

	mux.HandleFunc("GET /embed/_echo_headers", func(w http.ResponseWriter, r *http.Request) {
		// Exists for the acceptance check that the Cookie header is stripped AT
		// THE ROUTE rather than merely ignored here. __Host- forces Path=/, so
		// without the strip the session cookie arrives at this path on any
		// same-site navigation.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"cookie": r.Header.Get("Cookie"),
		})
	})

	mux.HandleFunc("GET /embed/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// frame-ancestors scoped to docs alone, and to this path alone. The
		// browser plane's own pages are frame-ancestors 'none'.
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; frame-ancestors https://docs.mindclade.dev; style-src 'unsafe-inline'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write([]byte("<!doctype html><title>embed</title>"))
	})

	return mux
}

func buildAuthenticated(d Deps) (http.Handler, error) {
	mux := http.NewServeMux()

	// Probes are registered OUTSIDE the authentication middleware below. A
	// readiness probe that has to pass IAP would fail every time, because the
	// kubelet carries no assertion — and the Deployment would never become
	// ready, with the pod logs showing only 401s.
	probes := http.NewServeMux()
	mountProbes(probes, d)

	switch d.Role {
	case RoleWeb:
		mountWeb(mux, d)
	case RoleBFF:
		mountBFF(mux, d)
	case RoleBFFStream:
		mountStream(mux, d)
	}

	authenticated := httpx.Authenticate(d.Verifier, d.Codec, d.Resolve, d.Logger)(
		httpx.CSRF(mux),
	)

	root := http.NewServeMux()
	root.Handle("GET /healthz", probes)
	root.Handle("GET /readyz", probes)
	root.Handle("/", httpx.SecurityHeaders(d.CSPMode, d.CSPReportURI, authenticated))

	return root, nil
}

func mountWeb(mux *http.ServeMux, d Deps) {
	// The catch-all. It also receives IAP's /_gcp_iap/* callback, which must
	// land on an IAP-ENABLED backend — that is why no route in the manifests
	// singles that path out, and why this role must keep its IAP policy.
	shell := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The nonce goes on every script tag. It comes from the per-response
		// CSP header, which is why the shell must never be cached.
		nonce := httpx.Nonce(r)
		_, _ = fmt.Fprintf(w,
			"<!doctype html><html><head><title>Mindclade Studio</title></head>"+
				"<body><div id=\"root\"></div>"+
				"<script nonce=%q src=\"/assets/app.js\"></script></body></html>", nonce)
	})

	mux.Handle("GET /", httpx.NoStoreShell(shell))
}

func mountBFF(mux *http.ServeMux, d Deps) {
	runs := runlog.NewStore(d.DB)
	handles := handoff.NewStore(d.DB)

	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		// Reaching here means the assertion verified and the session opened or
		// was reissued. The cookie was already refreshed by the middleware.
		principal, _ := httpx.Principal(r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"subject": principal})
	})

	mux.HandleFunc("GET /api/whoami", func(w http.ResponseWriter, r *http.Request) {
		principal, _ := httpx.Principal(r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"subject": principal})
	})

	mux.HandleFunc("POST /api/runs", func(w http.ResponseWriter, r *http.Request) {
		principal, _ := httpx.Principal(r)

		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			// Required, not optional. A GPU job is expensive enough that a
			// duplicate from a client retry is a real cost, and a retry is
			// exactly what a flaky connection produces.
			http.Error(w, "Idempotency-Key is required", http.StatusBadRequest)
			return
		}

		body, err := readBody(r)
		if err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		runID, created, err := runs.Submit(r.Context(), newUUID(), principal, key, digest(body))
		switch {
		case errors.Is(err, runlog.ErrDigestMismatch):
			// Same key, different body. A 409 rather than silently returning
			// the original run, which would discard what the caller asked for.
			http.Error(w, "idempotency key reused with a different body", http.StatusConflict)
			return
		case err != nil:
			d.Logger.Error("submit run", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted) // 202 whether created or replayed
		_ = json.NewEncoder(w).Encode(map[string]any{"run_id": runID, "created": created})
	})

	mux.HandleFunc("POST /api/runs/{runID}/cancel", func(w http.ResponseWriter, r *http.Request) {
		set, err := runs.RequestCancel(r.Context(), r.PathValue("runID"))
		if err != nil {
			d.Logger.Error("cancel run", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !set {
			http.Error(w, "already cancelled or terminal", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

	// Handoff redemption. Terminates in a 303 to /c/<docId>.
	mux.HandleFunc("GET /o/{handleID}", func(w http.ResponseWriter, r *http.Request) {
		principal, _ := httpx.Principal(r)

		result, err := handles.Redeem(r.Context(), r.PathValue("handleID"), principal,
			func(context.Context, string, json.RawMessage) error {
				// The authorization model is out of scope for this plan. Its
				// one interface is resource_ref, and the answer must be
				// computable inside the five-minute session cache.
				return nil
			},
			func(context.Context, string, json.RawMessage) (string, error) {
				return newUUID(), nil
			},
			func(ctx context.Context, e handoff.AuditEvent) {
				// To an APPEND-ONLY sink. A trail the audited service can
				// rewrite is decorative; this service holds insert and not
				// delete or update on the destination.
				d.Logger.InfoContext(ctx, "handoff.redemption",
					"handle_id", e.HandleID, "creator", e.Creator,
					"redeemer", e.Redeemer, "outcome", e.Outcome, "doc_id", e.DocID)
			})

		if errors.Is(err, handoff.ErrNotFound) {
			// 404 for a wrong principal, an expired handle, and one that never
			// existed — indistinguishable on purpose.
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			d.Logger.Error("redeem handoff", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// 303, and the SAME 303 on a repeat by the same principal — /o/<id> is
		// in browser history and a back navigation re-requests it.
		http.Redirect(w, r, "/c/"+result.DocID, http.StatusSeeOther)
	})
}

func mountStream(mux *http.ServeMux, d Deps) {
	runs := runlog.NewStore(d.DB)

	handler := stream.New(runs,
		func(_ context.Context, principal string, run runlog.RunMeta) error {
			if run.Submitter != principal {
				return errors.New("not the submitter")
			}
			return nil
		},
		httpx.Principal,
		d.Logger,
	)

	mux.Handle("GET /api/stream/runs/{runID}", handler)
}

// mountProbes registers liveness and readiness on mux.
//
// Both answers come from d.Health, so /readyz reports what it is named after:
// whether this process should be sent traffic. It previously returned a fixed
// 200, which meant a pod stayed in the Endpoints list through a database
// outage and through its own shutdown drain.
func mountProbes(mux *http.ServeMux, d Deps) {
	handler := health.NewHandler(d.Health, health.Config{
		LivenessPath:  "/healthz",
		ReadinessPath: "/readyz",
	})
	mux.Handle("GET /healthz", handler)
	mux.Handle("GET /readyz", handler)
	mux.Handle("HEAD /healthz", handler)
	mux.Handle("HEAD /readyz", handler)
}

// readBody reads a request body under a hard cap.
//
// MaxBytesReader rather than io.LimitReader: LimitReader truncates silently, so
// an oversized submission would be accepted with its tail discarded and hashed
// into a digest that matches nothing. MaxBytesReader errors instead.
func readBody(r *http.Request) ([]byte, error) {
	const maxBody = 1 << 20
	return io.ReadAll(http.MaxBytesReader(nil, r.Body, maxBody))
}

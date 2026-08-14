// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Command go_vanity serves the go-import meta tags for go.mindclade.dev.
//
// It is deliberately tiny and has no dependencies beyond the standard library.
// Every build in the organization resolves through it, so its failure modes
// should be exhaustible by reading it.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mindclade.internal/services/go_vanity/internal/vanity"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	addr := envOr("LISTEN_ADDRESS", ":8080")
	repoURL := envOr("MODULE_REPO_URL", "https://github.com/mindclade-org/mindclade")
	modulePath := envOr("MODULE_PATH", "go.mindclade.dev")
	docsURL := envOr("DOCS_URL", "https://docs.mindclade.dev")

	// One rule, because there is one module. A second module at, say,
	// go.mindclade.dev/tools is an additional rule here — the handler already
	// evaluates most-specific-first, so nothing else changes.
	handler, err := vanity.New(docsURL, vanity.Rule{
		Prefix:  modulePath,
		VCS:     "git",
		RepoURL: repoURL,
	})
	if err != nil {
		logger.Error("invalid vanity configuration", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Registered last and least specific, so the probes above win. The vanity
	// handler answers every other path by design — it cannot know which module
	// paths exist, so it answers uniformly and lets the clone decide.
	mux.Handle("/", handler)

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,

		// Modest timeouts. This server does nothing but render a template, so
		// a request outliving these is a client problem, not a slow response.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		// Drain rather than drop. A go build that loses its resolution mid-flight
		// fails with a network error naming neither this service nor the deploy
		// that caused it.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}()

	logger.Info("serving go-import meta tags",
		"address", addr, "module_path", modulePath, "repo_url", repoURL)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//
// Command studio is the browser plane. One image, four roles, four Services.
package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"mindclade.internal/services/studio/internal/httpx"
	"mindclade.internal/services/studio/internal/iap"
	"mindclade.internal/services/studio/internal/server"
	"mindclade.internal/services/studio/internal/session"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	role, err := server.ParseRole(os.Getenv("STUDIO_ROLE"))
	if err != nil {
		return err
	}
	logger = logger.With("role", string(role))

	deps := server.Deps{
		Role:         role,
		Logger:       logger,
		CSPMode:      cspMode(),
		CSPReportURI: os.Getenv("CSP_REPORT_URI"),
	}

	// The embed role deliberately gets no verifier, no codec, and no database.
	// It is cookieless and sessionless by design, and handing it credentials it
	// does not need would be the first step toward it quietly acquiring a
	// session.
	if role != server.RoleEmbed {
		if deps.Verifier, err = buildVerifier(); err != nil {
			return err
		}
		if deps.Codec, err = buildCodec(); err != nil {
			return err
		}

		// Authorization is out of scope for this plan. Its one interface is the
		// verified assertion; the answer must be computable inside the
		// five-minute session cache, which is what bounds how much can live
		// behind this.
		deps.Resolve = func(context.Context, iap.Assertion) error { return nil }

		if role != server.RoleWeb {
			if deps.DB, err = openDB(role); err != nil {
				return err
			}
			defer func() { _ = deps.DB.Close() }()
		}
	}

	handler, err := server.Build(deps)
	if err != nil {
		return err
	}

	addr := envOr("LISTEN_ADDRESS", ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,

		// NO WriteTimeout on the streaming role. A write deadline is absolute
		// from the start of the response, so any value at all would cut every
		// stream at that mark regardless of activity — which is precisely the
		// 30-second failure the 900s backend policy exists to avoid, moved
		// inside the process where no load-balancer setting can fix it.
		WriteTimeout: writeTimeout(role),
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		// A short drain is correct here BECAUSE streams are resumable. Without
		// that, terminationGracePeriodSeconds would have to exceed the longest
		// possible run; with it, a severed stream is a clean EOF the client
		// resumes from, and holding the pod open buys nothing.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}()

	logger.Info("listening", "address", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func writeTimeout(role server.Role) time.Duration {
	if role == server.RoleBFFStream {
		return 0
	}
	return 30 * time.Second
}

func cspMode() httpx.CSPMode {
	// Report-Only unless explicitly enforced. The default is the safe direction:
	// enforcing before a full Report-Only cycle breaks the structure viewer in
	// production rather than in a report.
	if os.Getenv("CSP_ENFORCE") == "true" {
		return httpx.CSPEnforce
	}
	return httpx.CSPReportOnly
}

func buildVerifier() (*iap.Verifier, error) {
	// The exact backend service this request reached:
	//   /projects/<project-number>/global/backendServices/<backend-service-id>
	//
	// Configuration rather than something derived at runtime, because the
	// workload cannot know its own backend service id — Terraform does. Getting
	// it wrong means accepting an assertion minted for a different IAP
	// application in the same organization.
	audience := os.Getenv("IAP_AUDIENCE")
	if audience == "" {
		return nil, errors.New("IAP_AUDIENCE is required; without it any IAP assertion in the organization would be accepted")
	}
	return iap.NewVerifier(audience, nil)
}

func buildCodec() (*session.Codec, error) {
	current, err := loadKey("SESSION_KEY_CURRENT_ID", "SESSION_KEY_CURRENT")
	if err != nil {
		return nil, err
	}

	// The previous key is OPTIONAL only for a first deployment. In steady state
	// both are set: accepting only the current key means every session issued
	// before the last rotation is rejected at once, which presents as a total
	// outage rather than as a rotation.
	var previous *session.Key
	if os.Getenv("SESSION_KEY_PREVIOUS") != "" {
		p, perr := loadKey("SESSION_KEY_PREVIOUS_ID", "SESSION_KEY_PREVIOUS")
		if perr != nil {
			return nil, perr
		}
		previous = &p
	}

	version, err := strconv.Atoi(envOr("AUTHZ_VERSION", "1"))
	if err != nil {
		return nil, fmt.Errorf("AUTHZ_VERSION must be an integer: %w", err)
	}

	return session.NewCodec(current, previous, version)
}

func loadKey(idVar, materialVar string) (session.Key, error) {
	id := os.Getenv(idVar)
	if id == "" {
		return session.Key{}, fmt.Errorf("%s is required", idVar)
	}
	raw := os.Getenv(materialVar)
	if raw == "" {
		return session.Key{}, fmt.Errorf("%s is required", materialVar)
	}
	material, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return session.Key{}, fmt.Errorf("%s must be base64: %w", materialVar, err)
	}
	return session.NewKey(id, material)
}

func openDB(role server.Role) (*sql.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	// CAP THE POOL BELOW Cloud SQL's maximum.
	//
	// The streaming tier holds long-lived, mostly-idle connections — each costs
	// a goroutine and, while tailing, a database cursor, but almost no CPU. Its
	// ceiling on simultaneous streams is therefore this pool, NOT the load
	// balancer, and exhausting it presents as latency on unrelated requests
	// rather than as an error naming the pool.
	//
	// Over capacity must be a clean refusal rather than a queue behind a
	// connection that will not arrive.
	maxOpen := 25
	if role == server.RoleBFFStream {
		maxOpen = 50
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxOpen / 2)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("database unreachable: %w", err)
	}
	return db, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

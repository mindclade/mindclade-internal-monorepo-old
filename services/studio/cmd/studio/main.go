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
	"net"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"

	"go.mindclade.dev/libs/go/clock"
	"go.mindclade.dev/libs/go/config"
	libhttpx "go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/servicekit"

	"go.mindclade.dev/services/studio/internal/authz"
	"go.mindclade.dev/services/studio/internal/httpx"
	"go.mindclade.dev/services/studio/internal/iap"
	"go.mindclade.dev/services/studio/internal/metrics"
	"go.mindclade.dev/services/studio/internal/server"
	"go.mindclade.dev/services/studio/internal/session"
)

// gracefulShutdownTimeout bounds the listener's graceful close, measured from
// AFTER the drain propagation window below.
//
// A short drain is correct here BECAUSE streams are resumable. Without that,
// terminationGracePeriodSeconds would have to exceed the longest possible run;
// with it, a severed stream is a clean EOF the client resumes from, and holding
// the pod open buys nothing.
const gracefulShutdownTimeout = 20 * time.Second

// serviceShutdownTimeout is the whole-process budget servicekit applies across
// drain AND stop, so it must cover both phases rather than either one.
//
// Under-setting this is the failure worth naming: the drain window would eat
// the budget and the listener would be closed by an expired context instead of
// by a graceful shutdown, which looks exactly like the 502s the drain exists to
// prevent.
const serviceShutdownTimeout = server.DrainPropagationDelay + gracefulShutdownTimeout + 5*time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(context.Background(), logger); err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	settings, err := loadSettings(ctx)
	if err != nil {
		return err
	}

	role, err := server.ParseRole(settings.MustGet(keyRole))
	if err != nil {
		return err
	}
	logger = logger.With("role", string(role))

	// Provenance, not just values. The digest identifies this exact
	// configuration across pods, and Redacted() is what makes logging the whole
	// set safe — the session keys and DSN below are declared Secret, so they
	// render as [REDACTED] rather than relying on nobody adding them to a log
	// line later.
	logger.Info("configuration loaded",
		"digest", settings.Digest().String(),
		"values", settings.Redacted())

	deps := server.Deps{
		Role:         role,
		Logger:       logger,
		CSPMode:      cspMode(settings),
		CSPReportURI: value(settings, keyCSPReportURI),
	}

	// The embed role deliberately gets no verifier, no codec, and no database.
	// It is cookieless and sessionless by design, and handing it credentials it
	// does not need would be the first step toward it quietly acquiring a
	// session.
	if role != server.RoleEmbed {
		if deps.Verifier, err = buildVerifier(settings); err != nil {
			return err
		}
		if deps.Codec, err = buildCodec(settings); err != nil {
			return err
		}

		policy, policyErr := authz.New(value(settings, keyAuthorizedSubjects))
		if policyErr != nil {
			return fmt.Errorf("AUTHORIZED_IAP_SUBJECTS must contain at least one exact stable IAP subject: %w", policyErr)
		}
		deps.Resolve = policy.Resolve

		if role != server.RoleWeb {
			if deps.DB, err = openDB(settings, role); err != nil {
				return err
			}
			defer func() { _ = deps.DB.Close() }()
		}
	}

	if deps.Health, err = server.NewHealth(deps.DB); err != nil {
		return err
	}
	if deps.Metrics, err = buildMetrics(); err != nil {
		return err
	}

	handler, err := server.Build(deps)
	if err != nil {
		return err
	}

	addr := settings.MustGet(keyListenAddress)
	httpServer, err := libhttpx.NewServer(handler, libhttpx.ServerConfig{
		Addr:              addr,
		ReadHeaderTimeout: 10 * time.Second,

		// NO WriteTimeout on the streaming role. A write deadline is absolute
		// from the start of the response, so any value at all would cut every
		// stream at that mark regardless of activity — which is precisely the
		// 30-second failure the 900s backend policy exists to avoid, moved
		// inside the process where no load-balancer setting can fix it.
		WriteTimeout:    writeTimeout(role),
		IdleTimeout:     120 * time.Second,
		ShutdownTimeout: gracefulShutdownTimeout,
	})
	if err != nil {
		return err
	}

	// Bound before the lifecycle starts, so a taken port fails here with the
	// address in the error rather than midway through a start sequence that has
	// already opened a database pool.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	service, err := buildService(deps.Health, httpServer, listener, logger, clock.RealClock{})
	if err != nil {
		return err
	}

	logger.Info("listening", "address", addr)
	return service.RunWithSignals(ctx)
}

// buildService assembles the process lifecycle.
//
// Separate from run so the drain-then-stop ordering below can be tested against
// a fake clock rather than by waiting out the real propagation window.
func buildService(
	health *server.Health,
	httpServer *libhttpx.Server,
	listener net.Listener,
	logger *slog.Logger,
	timeSource clock.Clock,
) (*servicekit.Service, error) {
	service, err := servicekit.New("studio",
		servicekit.WithShutdownTimeout(serviceShutdownTimeout),
		// Must exceed the propagation delay the drain component waits out, or
		// servicekit would cancel the very wait it is there to perform.
		servicekit.WithComponentDrainTimeout(server.DrainPropagationDelay+5*time.Second),
		servicekit.WithComponentStopTimeout(gracefulShutdownTimeout),
		// One clock for the whole process. A component measuring its own wait
		// on a different time source than the coordinator enforcing its budget
		// is how a drain silently outlives the deadline meant to bound it.
		servicekit.WithClock(timeSource),
	)
	if err != nil {
		return nil, err
	}

	// What sequences these is servicekit's PHASE separation, not their
	// registration order: every component's Drain runs before any component's
	// Stop. So the gate below fails readiness and waits out the propagation
	// window while the HTTP component is still serving, and only then is the
	// listener gracefully closed.
	//
	// Registration order is genuinely irrelevant here — the gate has no Stop
	// and the HTTP component has no Drain, so the two never contend within a
	// phase. Adding a Stop to the gate or a Drain to the HTTP component would
	// change that, since both phases run in reverse registration order.
	if err := service.Add(drainGate(health, logger, timeSource)); err != nil {
		return nil, err
	}
	if err := service.Add(httpServer.Component("http", listener)); err != nil {
		return nil, err
	}
	return service, nil
}

// drainGate fails readiness and then waits, without closing anything.
//
// FAIL READINESS FIRST, then keep serving for the propagation window.
//
// Endpoints removal is not synchronous with the probe flipping, so closing the
// listener at signal time would strand whatever the load balancer is still
// routing to this pod as 502s. The process is healthy for this whole delay — it
// is only telling the orchestrator to stop aiming at it, and waiting for that
// to take effect.
func drainGate(health *server.Health, logger *slog.Logger, timeSource clock.Clock) servicekit.Component {
	return servicekit.Component{
		Name: "readiness-drain",
		Drain: func(ctx context.Context) error {
			health.BeginDrain()
			logger.Info("draining", "propagation_delay", server.DrainPropagationDelay.String())

			// Sleep honors cancellation rather than waiting blind: if the
			// shutdown budget is already spent, waiting the full window would
			// only push the graceful close past its own deadline.
			return timeSource.Sleep(ctx, server.DrainPropagationDelay)
		},
	}
}

// buildMetrics registers what this process exposes on /metrics.
//
// Registration is checked rather than ignored: a collector that cannot produce
// a valid measurement is a wiring bug, and failing here beats serving a
// malformed scrape for the life of the deployment.
func buildMetrics() (*metrics.Registry, error) {
	registry := metrics.NewRegistry()
	if err := registry.Register(
		httpx.SessionDecryptFailureMetricName,
		"Session cookies that could not be opened. Alert on the rate against a baseline, not on any single failure: ordinary expiry is indistinguishable from a bad key rotation at this layer.",
		httpx.SessionDecryptFailureMetric,
	); err != nil {
		return nil, err
	}
	return registry, nil
}

func writeTimeout(role server.Role) time.Duration {
	if role == server.RoleBFFStream {
		return 0
	}
	return 30 * time.Second
}

func cspMode(settings config.Snapshot) httpx.CSPMode {
	// Report-Only unless explicitly enforced. The default is the safe direction:
	// enforcing before a full Report-Only cycle breaks the structure viewer in
	// production rather than in a report.
	if settings.MustGet(keyCSPEnforce) == "true" {
		return httpx.CSPEnforce
	}
	return httpx.CSPReportOnly
}

func buildVerifier(settings config.Snapshot) (*iap.Verifier, error) {
	// The exact backend service this request reached:
	//   /projects/<project-number>/global/backendServices/<backend-service-id>
	//
	// Configuration rather than something derived at runtime, because the
	// workload cannot know its own backend service id — Terraform does. Getting
	// it wrong means accepting an assertion minted for a different IAP
	// application in the same organization.
	//
	// Declared optional in the schema and required HERE, because the embed role
	// legitimately runs without it. A schema-level Required would force every
	// role to carry a credential one of them must not have.
	audience := value(settings, keyIAPAudience)
	if audience == "" {
		return nil, errors.New("IAP_AUDIENCE is required; without it any IAP assertion in the organization would be accepted")
	}
	return iap.NewVerifier(audience, nil)
}

func buildCodec(settings config.Snapshot) (*session.Codec, error) {
	current, err := loadKey(settings, keySessionKeyCurrentID, keySessionKeyCurrent)
	if err != nil {
		return nil, err
	}

	// The previous key is OPTIONAL only for a first deployment. In steady state
	// both are set: accepting only the current key means every session issued
	// before the last rotation is rejected at once, which presents as a total
	// outage rather than as a rotation.
	var previous *session.Key
	if value(settings, keySessionKeyPrevious) != "" {
		p, perr := loadKey(settings, keySessionKeyPreviousID, keySessionKeyPrevious)
		if perr != nil {
			return nil, perr
		}
		previous = &p
	}

	// Already validated as an integer by the schema; the error is impossible
	// here and reported rather than dropped so a schema change cannot silently
	// turn it into a zero.
	version, err := strconv.Atoi(settings.MustGet(keyAuthzVersion))
	if err != nil {
		return nil, fmt.Errorf("AUTHZ_VERSION must be an integer: %w", err)
	}

	return session.NewCodec(current, previous, version)
}

func loadKey(settings config.Snapshot, idKey, materialKey string) (session.Key, error) {
	id := value(settings, idKey)
	if id == "" {
		return session.Key{}, fmt.Errorf("%s is required", envName(idKey))
	}
	raw := value(settings, materialKey)
	if raw == "" {
		return session.Key{}, fmt.Errorf("%s is required", envName(materialKey))
	}
	material, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return session.Key{}, fmt.Errorf("%s must be base64: %w", envName(materialKey), err)
	}
	return session.NewKey(id, material)
}

func openDB(settings config.Snapshot, role server.Role) (*sql.DB, error) {
	dsn := value(settings, keyDatabaseURL)
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

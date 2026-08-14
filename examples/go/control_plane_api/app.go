// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package controlplaneapi is a runnable local integration showing how a Go
// control-plane API composes the Mindclade Go foundation. Production services
// replace the in-memory adapters with the PostgreSQL and broker adapters while
// retaining the same servicekit and coordination contracts.
package controlplaneapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"mindclade.internal/libs/go/audit"
	"mindclade.internal/libs/go/coordination/outbox"
	outboxmemory "mindclade.internal/libs/go/coordination/outbox/memory"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/httpx"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/messaging"
	messagingmemory "mindclade.internal/libs/go/messaging/memory"
	"mindclade.internal/libs/go/requestmeta"
	"mindclade.internal/libs/go/servicekit"
)

const (
	defaultAddress = "127.0.0.1:0"
	maxRequestBody = 64 << 10
)

var runIDKind = identifiers.MustParseKind("run")

// Config contains the deliberately small process-level configuration surface.
type Config struct {
	Address string
}

// Run is the durable resource returned by this example API.
type Run struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// App owns the process composition and its local integration adapters.
type App struct {
	listener net.Listener
	server   *httpx.Server
	service  *servicekit.Service

	outboxStore   *outboxmemory.Store
	outboxFactory *outbox.Factory
	broker        *messagingmemory.Broker
	subscription  messaging.Subscription
	auditFactory  *audit.Factory
	auditActor    audit.Actor

	mu          sync.RWMutex
	runs        map[string]Run
	auditEvents []audit.Event
	events      chan messaging.Message
}

// New creates a fully wired local process. It fails closed if any production
// mechanism cannot be constructed.
func New(config Config) (*App, error) {
	address := strings.TrimSpace(config.Address)
	if address == "" {
		address = defaultAddress
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeUnavailable, "unable to open control-plane listener", faults.WithReason("listener_open_failed"), faults.WithOperation("examples.control_plane_api.New"))
	}
	closeListener := true
	defer func() {
		if closeListener {
			_ = listener.Close()
		}
	}()

	outboxStore, err := outboxmemory.New()
	if err != nil {
		return nil, err
	}
	outboxFactory, err := outbox.NewFactory()
	if err != nil {
		return nil, err
	}
	broker, err := messagingmemory.NewBroker(messagingmemory.Config{Capacity: 128, MaxAttempts: 5, AckDeadline: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	subscription, err := broker.Subscribe("runs.created")
	if err != nil {
		return nil, err
	}
	auditFactory, err := audit.NewFactory()
	if err != nil {
		return nil, err
	}
	auditActor, err := audit.NewSystemActor("examples/control-plane-api")
	if err != nil {
		return nil, err
	}

	app := &App{
		listener:      listener,
		outboxStore:   outboxStore,
		outboxFactory: outboxFactory,
		broker:        broker,
		subscription:  subscription,
		auditFactory:  auditFactory,
		auditActor:    auditActor,
		runs:          make(map[string]Run),
		events:        make(chan messaging.Message, 32),
	}

	dispatcher, err := outbox.NewDispatcher(
		outboxStore,
		outbox.PublisherFunc(app.publishOutbox),
		outbox.DispatcherConfig{
			Owner:         "examples-control-plane-dispatcher",
			Topics:        []string{"runs.created"},
			PollInterval:  5 * time.Millisecond,
			ClaimDuration: 5 * time.Second,
			BatchSize:     32,
			IdleReady:     true,
		},
	)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", app.handleLiveness)
	mux.HandleFunc("GET /readyz", app.handleReadiness)
	mux.HandleFunc("POST /v1/runs", app.handleCreateRun)
	mux.HandleFunc("GET /v1/runs/{run_id}", app.handleGetRun)
	server, err := httpx.NewServer(mux, httpx.ServerConfig{ShutdownTimeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	app.server = server

	assembly, err := servicekit.NewAssembly(
		"examples-control-plane-api",
		servicekit.WithStartupTimeout(10*time.Second),
		servicekit.WithShutdownTimeout(10*time.Second),
		servicekit.WithComponentDrainTimeout(5*time.Second),
		servicekit.WithComponentStopTimeout(5*time.Second),
	)
	if err != nil {
		return nil, err
	}
	if err := assembly.AddFoundation(servicekit.Component{
		Name:      "audit-recorder",
		Liveness:  func(context.Context) error { return nil },
		Readiness: func(context.Context) error { return nil },
	}); err != nil {
		return nil, err
	}
	if err := assembly.AddCoordination(dispatcher.Component("outbox-dispatcher")); err != nil {
		return nil, err
	}
	if err := assembly.AddWork(app.subscriptionComponent()); err != nil {
		return nil, err
	}
	httpComponent := server.Component("http-server", listener)
	httpComponent.Drain = func(ctx context.Context) error { return server.Shutdown(ctx) }
	if err := assembly.AddServing(httpComponent); err != nil {
		return nil, err
	}
	service, err := assembly.Build()
	if err != nil {
		return nil, err
	}
	app.service = service
	closeListener = false
	return app, nil
}

// Service exposes the lifecycle coordinator for process entrypoints and tests.
func (app *App) Service() *servicekit.Service { return app.service }

// Address returns the bound listener address.
func (app *App) Address() string {
	if app == nil || app.listener == nil {
		return ""
	}
	return app.listener.Addr().String()
}

// NextEvent waits for the next locally delivered event.
func (app *App) NextEvent(ctx context.Context) (messaging.Message, error) {
	if app == nil || ctx == nil {
		return messaging.Message{}, faults.New(faults.CodeInvalidArgument, "event context is required", faults.WithReason("nil_event_context"))
	}
	select {
	case <-ctx.Done():
		return messaging.Message{}, ctx.Err()
	case event := <-app.events:
		return event, nil
	}
}

// AuditEvents returns a defensive snapshot of recorded audit events.
func (app *App) AuditEvents() []audit.Event {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return append([]audit.Event(nil), app.auditEvents...)
}

func (app *App) publishOutbox(ctx context.Context, value outbox.Message) error {
	id, err := identifiers.NewID(messaging.MessageIDKind)
	if err != nil {
		return err
	}
	attributes := value.Headers()
	if attributes == nil {
		attributes = make(map[string]string)
	}
	attributes["outbox_id"] = value.ID().String()
	message, err := messaging.NewMessage(
		id,
		value.Topic(),
		value.PartitionKey(),
		value.ContentType(),
		value.Payload(),
		attributes,
		value.Request(),
		value.CreatedAt(),
	)
	if err != nil {
		return err
	}
	_, err = app.broker.Publish(ctx, message)
	return err
}

func (app *App) subscriptionComponent() servicekit.Component {
	return servicekit.Component{
		Name: "runs-created-projector",
		Run: func(ctx context.Context) error {
			return app.subscription.Receive(ctx, func(_ context.Context, delivery messaging.Delivery) error {
				select {
				case app.events <- delivery.Message():
					return nil
				default:
					return faults.New(faults.CodeResourceExhausted, "example event buffer is full", faults.WithReason("event_buffer_full"), faults.WithRetryPolicy(faults.BackoffRetry(3)))
				}
			})
		},
		Drain: func(ctx context.Context) error { return app.subscription.Close(ctx) },
		Stop:  func(ctx context.Context) error { return app.broker.Close(ctx) },
		Liveness: func(context.Context) error {
			if app.subscription == nil {
				return faults.New(faults.CodeUnavailable, "event subscription is unavailable", faults.WithReason("subscription_unavailable"))
			}
			return nil
		},
		Readiness: func(context.Context) error { return nil },
	}
}

func (app *App) handleCreateRun(writer http.ResponseWriter, request *http.Request) {
	ctx, _, err := requestmeta.EnsureRequestID(request.Context())
	if err != nil {
		httpx.WriteError(request.Context(), writer, err, request.URL.Path)
		return
	}
	ctx, err = requestmeta.WithOperation(ctx, requestmeta.MustParseOperation("runs.create"))
	if err != nil {
		httpx.WriteError(request.Context(), writer, err, request.URL.Path)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	request = request.WithContext(ctx)
	if err := httpx.DecodeJSON(request, &input, maxRequestBody); err != nil {
		httpx.WriteError(ctx, writer, err, request.URL.Path)
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 128 {
		httpx.WriteError(ctx, writer, faults.New(faults.CodeInvalidArgument, "run name is required", faults.WithReason("invalid_run_name"), faults.WithContextMetadata(ctx)), request.URL.Path)
		return
	}

	id, err := identifiers.NewID(runIDKind)
	if err != nil {
		httpx.WriteError(ctx, writer, err, request.URL.Path)
		return
	}
	run := Run{ID: id.String(), Name: input.Name, CreatedAt: time.Now().UTC().Round(0)}
	payload, err := json.Marshal(run)
	if err != nil {
		httpx.WriteError(ctx, writer, faults.Wrap(err, faults.CodeInternal, "unable to encode run", faults.WithReason("run_encode_failed")), request.URL.Path)
		return
	}
	metadata, _ := requestmeta.FromContext(ctx)
	message, err := app.outboxFactory.Create("runs.created", run.ID, "application/json", payload, map[string]string{"schema": "runs.created.v1"}, metadata, time.Time{})
	if err != nil {
		httpx.WriteError(ctx, writer, err, request.URL.Path)
		return
	}

	actor := app.auditActor
	target, err := audit.NewTarget("run", audit.WithTargetID(id), audit.WithTargetName(input.Name))
	if err != nil {
		httpx.WriteError(ctx, writer, err, request.URL.Path)
		return
	}
	event, err := app.auditFactory.Create(audit.MustParseAction("runs.create"), actor, target, audit.OutcomeSucceeded, audit.WithRequestMetadata(metadata))
	if err != nil {
		httpx.WriteError(ctx, writer, err, request.URL.Path)
		return
	}

	// The example uses one critical section to model the transaction boundary.
	// Production code performs the resource write, audit append, and outbox append
	// inside one PostgreSQL transaction.
	app.mu.Lock()
	app.runs[run.ID] = run
	app.auditEvents = append(app.auditEvents, event)
	app.mu.Unlock()
	if err := app.outboxStore.Append(ctx, message); err != nil {
		app.mu.Lock()
		delete(app.runs, run.ID)
		app.auditEvents = app.auditEvents[:len(app.auditEvents)-1]
		app.mu.Unlock()
		httpx.WriteError(ctx, writer, err, request.URL.Path)
		return
	}
	_ = httpx.WriteJSON(writer, http.StatusCreated, run)
}

func (app *App) handleGetRun(writer http.ResponseWriter, request *http.Request) {
	identifier := request.PathValue("run_id")
	if _, err := identifiers.ParseIDKind(identifier, runIDKind); err != nil {
		httpx.WriteError(request.Context(), writer, faults.Wrap(err, faults.CodeInvalidArgument, "invalid run identifier", faults.WithReason("invalid_run_id")), request.URL.Path)
		return
	}
	app.mu.RLock()
	run, ok := app.runs[identifier]
	app.mu.RUnlock()
	if !ok {
		httpx.WriteError(request.Context(), writer, faults.New(faults.CodeNotFound, "run not found", faults.WithReason("run_not_found")), request.URL.Path)
		return
	}
	_ = httpx.WriteJSON(writer, http.StatusOK, run)
}

func (app *App) handleLiveness(writer http.ResponseWriter, request *http.Request) {
	if app.service == nil || !app.service.Liveness(request.Context()).OK {
		_ = httpx.WriteJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "failed"})
		return
	}
	_ = httpx.WriteJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (app *App) handleReadiness(writer http.ResponseWriter, request *http.Request) {
	if app.service == nil || !app.service.Readiness(request.Context()).OK {
		_ = httpx.WriteJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "failed"})
		return
	}
	_ = httpx.WriteJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

// Run starts the assembled service until ctx cancellation or a component error.
func (app *App) Run(ctx context.Context) error {
	if app == nil || app.service == nil {
		return faults.New(faults.CodeFailedPrecondition, "control-plane example is not initialized", faults.WithReason("app_uninitialized"))
	}
	if ctx == nil {
		return faults.New(faults.CodeInvalidArgument, "run context is required", faults.WithReason("nil_context"))
	}
	return app.service.Run(ctx)
}

// IsExpectedShutdown reports whether err is a normal context-driven shutdown.
func IsExpectedShutdown(err error) bool {
	return err == nil || errors.Is(err, context.Canceled)
}

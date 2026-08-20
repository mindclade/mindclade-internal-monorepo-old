// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package transport_foundation exercises the three libs/go transport test
// harnesses from outside their own packages.
//
// connecttest, grpctest, and httpxtest exist to be used by other packages'
// tests, so they have the same problem the Kubernetes reconcile helpers had:
// no production code can import them, and a test inside `package connecttest`
// is the package rather than a caller of it. Nothing had ever driven them.
//
// This suite is that caller, and it drives each harness the way a real
// transport test would -- against the same health and problem surfaces the api
// role actually serves, rather than against a stub invented for the test.
package transport_foundation

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"go.mindclade.dev/libs/go/connectx/connecttest"
	connecthealth "go.mindclade.dev/libs/go/connectx/health"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/grpcx/grpctest"
	grpchealth "go.mindclade.dev/libs/go/grpcx/health"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/httpx/httpxtest"
	"go.mindclade.dev/libs/go/servicekit"
)

// healthService is the service the api role advertises, reused here so the
// harnesses are driven against a real name rather than a fictional one.
const healthService = "grpc.health.v1.Health"

// prober is the readiness source both health surfaces read. It satisfies the
// connectx and grpcx prober contracts, which are structurally identical.
type prober struct{ ok bool }

func (value prober) Readiness(context.Context) servicekit.ProbeReport {
	return servicekit.ProbeReport{OK: value.ok, CheckedAt: time.Unix(1_800_000_000, 0).UTC()}
}

// httpxtest.Server plus DecodeProblem is the pairing the harness exists for:
// a handler that fails should put an RFC 7807 problem on the wire, and the
// decoder should read back the structured fields rather than a blob of JSON.
func TestHTTPHarnessDecodesAProblem(t *testing.T) {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		httpx.WriteError(request.Context(), writer, faults.New(
			faults.CodeNotFound,
			"workspace does not exist",
			faults.WithReason("workspace_not_found"),
			faults.WithOperation("controlplane.workspaces.get"),
		), request.URL.Path)
	})

	server := httpxtest.Server(t, handler)
	response, err := server.Client().Get(server.URL + "/workspaces/missing")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d", response.StatusCode)
	}

	problem := httpxtest.DecodeProblem(t, response)
	if problem.Status != http.StatusNotFound {
		t.Fatalf("problem status=%d", problem.Status)
	}
	if problem.Reason != "workspace_not_found" {
		t.Fatalf("reason=%q", problem.Reason)
	}
	if problem.Operation != "controlplane.workspaces.get" {
		t.Fatalf("operation=%q", problem.Operation)
	}
	// The instance is what ties a problem to the request that caused it.
	if problem.Instance != "/workspaces/missing" {
		t.Fatalf("instance=%q", problem.Instance)
	}
}

// Retryability has to survive the round trip. It is the one field a caller
// acts on automatically, so a decoder that dropped it would turn a retryable
// outage into a hard failure at every client.
func TestHTTPHarnessReadsRetryability(t *testing.T) {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		httpx.WriteError(request.Context(), writer, faults.New(
			faults.CodeUnavailable,
			"database is unreachable",
			faults.WithReason("database_unavailable"),
			faults.WithRetryPolicy(faults.ImmediateRetry(3)),
		), request.URL.Path)
	})

	server := httpxtest.Server(t, handler)
	response, err := server.Client().Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if problem := httpxtest.DecodeProblem(t, response); !problem.Retryable {
		t.Fatalf("problem=%+v", problem)
	}
}

// connecttest.Server is HTTP/2 over TLS, which is what makes it the right
// harness for Connect: the h2c-free path a real client negotiates. The
// Recorder is driven alongside it so the suite covers both halves.
func TestConnectHarnessServesHealthAndRecordsTheCall(t *testing.T) {
	checker, err := connecthealth.NewChecker(prober{ok: true}, healthService)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &connecttest.Recorder{}
	path, handler := connecthealth.NewHandler(checker, connect.WithInterceptors(recorder.Interceptor()))

	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := connecttest.Server(t, mux)

	// The Connect protocol over JSON: no generated client is needed to prove
	// the handler is mounted, negotiated, and answering.
	response, err := server.Client().Post(
		server.URL+path+"Check", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	if !strings.HasPrefix(response.Proto, "HTTP/2") {
		t.Fatalf("proto=%q, want HTTP/2 from the TLS harness", response.Proto)
	}

	calls := recorder.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls=%+v", calls)
	}
	if !strings.HasSuffix(calls[0].Procedure, "/Check") {
		t.Fatalf("procedure=%q", calls[0].Procedure)
	}
	// The interceptor is mounted on the handler, so this is the server half.
	if calls[0].Client {
		t.Fatal("recorder attributed a handler call to a client")
	}
}

// An unready process must answer NOT_SERVING rather than fail the call: a
// health check that errors is indistinguishable from one that cannot be
// reached, and an orchestrator treats those differently.
func TestConnectHarnessReportsUnreadyWithoutFailing(t *testing.T) {
	checker, err := connecthealth.NewChecker(prober{ok: false}, healthService)
	if err != nil {
		t.Fatal(err)
	}
	path, handler := connecthealth.NewHandler(checker)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := connecttest.Server(t, mux)

	response, err := server.Client().Post(
		server.URL+path+"Check", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}

	var decoded struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// grpchealth serializes the enum by its full proto name, not the bare
	// NOT_SERVING an operator reads in grpcurl output.
	if decoded.Status != "SERVING_STATUS_NOT_SERVING" {
		t.Fatalf("status=%q, want SERVING_STATUS_NOT_SERVING", decoded.Status)
	}
}

// grpctest.Start plus Client is the bufconn pairing: a real gRPC server and a
// real client connection with no port, so the health service is exercised over
// the actual wire protocol rather than by calling the handler directly.
func TestGRPCHarnessServesHealth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := grpc.NewServer()
	if _, err := grpchealth.Register(server, prober{ok: true}, grpchealth.Config{
		Services: []string{healthService},
	}); err != nil {
		t.Fatal(err)
	}

	harness := grpctest.Start(t, server)
	connection, err := harness.Client(ctx)
	if err != nil {
		t.Fatalf("Client(): %v", err)
	}
	defer connection.Close()

	response, err := healthpb.NewHealthClient(connection).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check(): %v", err)
	}
	if response.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("status=%s", response.GetStatus())
	}
}

// A service the server never registered is NotFound, not SERVING. Without this
// the harness would pass against a server that reports everything healthy.
func TestGRPCHarnessRejectsAnUnknownService(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := grpc.NewServer()
	if _, err := grpchealth.Register(server, prober{ok: true}, grpchealth.Config{
		Services: []string{healthService},
	}); err != nil {
		t.Fatal(err)
	}
	harness := grpctest.Start(t, server)
	connection, err := harness.Client(ctx)
	if err != nil {
		t.Fatalf("Client(): %v", err)
	}
	defer connection.Close()

	_, err = healthpb.NewHealthClient(connection).Check(ctx, &healthpb.HealthCheckRequest{
		Service: "mindclade.control.v1.NotRegistered",
	})
	if err == nil {
		t.Fatal("Check() on an unregistered service returned nil")
	}
}

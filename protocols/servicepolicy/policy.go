// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

// Package servicepolicy owns transport-level policy for promoted protobuf RPCs.
package servicepolicy

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/faults"
)

const MaximumRequestBytes = 1 << 20

type Idempotency string

const (
	IdempotencyRead           Idempotency = "read"
	IdempotencyRequired       Idempotency = "required"
	IdempotencyStreamRequired Idempotency = "stream_required"
)

type Policy struct {
	Permission      auth.Permission
	ResourceType    auth.ResourceType
	DefaultDeadline time.Duration
	MaximumDeadline time.Duration
	MaximumAttempts uint8
	Idempotency     Idempotency
}

func read(permission, resource string) Policy {
	return policy(permission, resource, 10*time.Second, 30*time.Second, 3, IdempotencyRead)
}

func mutate(permission, resource string) Policy {
	return policy(permission, resource, 30*time.Second, time.Minute, 1, IdempotencyRequired)
}

func execute(permission, resource string) Policy {
	return policy(permission, resource, time.Minute, 5*time.Minute, 1, IdempotencyStreamRequired)
}

func policy(permission, resource string, defaultDeadline, maximumDeadline time.Duration, attempts uint8, idempotency Idempotency) Policy {
	parsedResource, err := auth.ParseResourceType(resource)
	if err != nil {
		panic(err)
	}
	return Policy{
		Permission: auth.MustParsePermission(permission), ResourceType: parsedResource,
		DefaultDeadline: defaultDeadline, MaximumDeadline: maximumDeadline,
		MaximumAttempts: attempts, Idempotency: idempotency,
	}
}

var policies = map[string]Policy{
	"/mindclade.artifact.v1.ArtifactService/ListArtifacts":       read("artifacts.read", "artifact"),
	"/mindclade.artifact.v1.ArtifactService/GetArtifact":         read("artifacts.read", "artifact"),
	"/mindclade.data.v1.DatasetService/ListDatasets":             read("datasets.read", "dataset"),
	"/mindclade.data.v1.DatasetService/GetDataset":               read("datasets.read", "dataset"),
	"/mindclade.evaluation.v1.EvaluationService/ListEvaluations": read("evaluations.read", "evaluation"),
	"/mindclade.evaluation.v1.EvaluationService/GetEvaluation":   read("evaluations.read", "evaluation"),
	"/mindclade.inference.v1.InferenceService/Predict":           mutate("inference.execute", "inference.request"),
	"/mindclade.inference.v1.InferenceService/StreamPredict":     execute("inference.execute", "inference.request"),
	"/mindclade.orchestration.v1.RunService/ListRuns":            read("runs.read", "run"),
	"/mindclade.orchestration.v1.RunService/GetRun":              read("runs.read", "run"),
	"/mindclade.orchestration.v1.RunService/CreateRun":           mutate("runs.create", "run"),
	"/mindclade.orchestration.v1.RunService/CancelRun":           mutate("runs.cancel", "run"),
	"/mindclade.registry.v1.ModelRegistryService/ListModels":     read("registry.models.read", "model"),
	"/mindclade.registry.v1.ModelRegistryService/GetModel":       read("registry.models.read", "model"),
	"/mindclade.runtime.v1.RuntimeExecution/Execute":             execute("runtime.execute", "runtime.execution"),
	"/mindclade.runtime.v1.RuntimePolicyFeed/GetRouteSnapshot":   read("runtime.policy.read", "runtime.policy"),
	"/mindclade.runtime.v1.RuntimePolicyFeed/GetRevocations":     read("runtime.policy.read", "runtime.policy"),
	"/mindclade.runtime.v1.WorkerControl/Execute":                execute("runtime.worker.control", "runtime.worker"),
}

func Lookup(fullMethod string) (Policy, bool) {
	value, ok := policies[fullMethod]
	return value, ok
}

func All() map[string]Policy {
	result := make(map[string]Policy, len(policies))
	for method, value := range policies {
		result[method] = value
	}
	return result
}

func Public(fullMethod string) bool {
	return strings.HasPrefix(fullMethod, "/grpc.health.v1.Health/") ||
		strings.HasPrefix(fullMethod, "/grpc.reflection.v1.ServerReflection/")
}

func UnaryEnforcement() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		policy, ok := Lookup(info.FullMethod)
		if !ok {
			return handler(ctx, request)
		}
		ctx, cancel := boundedDeadline(ctx, policy)
		defer cancel()
		if err := requireIdempotency(ctx, policy); err != nil {
			return nil, err
		}
		return handler(ctx, request)
	}
}

func StreamEnforcement() grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		policy, ok := Lookup(info.FullMethod)
		if !ok {
			return handler(server, stream)
		}
		ctx, cancel := boundedDeadline(stream.Context(), policy)
		defer cancel()
		if err := requireIdempotency(ctx, policy); err != nil {
			return err
		}
		return handler(server, &contextStream{ServerStream: stream, ctx: ctx})
	}
}

// UnaryClientEnforcement applies the same deadline and idempotency contract at
// the caller boundary and performs bounded retries only for declared reads.
// Mutations are never replayed by transport policy, even when they carry an
// idempotency key: the service remains the authority for replay semantics.
func UnaryClientEnforcement() grpc.UnaryClientInterceptor {
	return unaryClientEnforcement(waitForRetry)
}

func unaryClientEnforcement(wait func(context.Context, time.Duration) error) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request, reply any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
		policy, ok := Lookup(method)
		if !ok {
			return invoker(ctx, method, request, reply, connection, options...)
		}
		ctx, cancel := boundedDeadline(ctx, policy)
		defer cancel()
		if err := requireOutgoingIdempotency(ctx, policy); err != nil {
			return err
		}
		attempts := policy.MaximumAttempts
		if policy.Idempotency != IdempotencyRead {
			attempts = 1
		}
		var err error
		for attempt := uint8(1); attempt <= attempts; attempt++ {
			err = invoker(ctx, method, request, reply, connection, options...)
			if err == nil || attempt == attempts || !retryable(status.Code(err)) {
				return err
			}
			delay := 25 * time.Millisecond * time.Duration(1<<(attempt-1))
			if err := wait(ctx, delay); err != nil {
				return err
			}
		}
		return err
	}
}

func retryable(code codes.Code) bool {
	return code == codes.Unavailable || code == codes.ResourceExhausted
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func boundedDeadline(ctx context.Context, policy Policy) (context.Context, context.CancelFunc) {
	limit := policy.DefaultDeadline
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= policy.MaximumDeadline {
			return context.WithCancel(ctx)
		}
		limit = policy.MaximumDeadline
	}
	return context.WithTimeout(ctx, limit)
}

func requireIdempotency(ctx context.Context, policy Policy) error {
	if policy.Idempotency == IdempotencyRead {
		return nil
	}
	return validateIdempotencyValues(metadata.ValueFromIncomingContext(ctx, "idempotency-key"))
}

func requireOutgoingIdempotency(ctx context.Context, policy Policy) error {
	if policy.Idempotency == IdempotencyRead {
		return nil
	}
	outgoing, _ := metadata.FromOutgoingContext(ctx)
	values := outgoing.Get("idempotency-key")
	return validateIdempotencyValues(values)
}

func validateIdempotencyValues(values []string) error {
	if len(values) != 1 {
		return faults.New(
			faults.CodeInvalidArgument,
			"exactly one idempotency key is required",
			faults.WithReason("idempotency_key_required"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	value := strings.TrimSpace(values[0])
	if len(value) < 8 || len(value) > 256 || strings.ContainsAny(value, "\r\n\x00") {
		return faults.New(
			faults.CodeInvalidArgument,
			"idempotency key is invalid",
			faults.WithReason("idempotency_key_invalid"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	return nil
}

type contextStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *contextStream) Context() context.Context { return stream.ctx }

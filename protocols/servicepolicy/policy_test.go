// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package servicepolicy

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"go.mindclade.dev/libs/go/faults"
)

func TestEveryPolicyIsBoundedAndSafe(t *testing.T) {
	if len(All()) != 18 {
		t.Fatalf("policy count = %d, want 18", len(All()))
	}
	for method, policy := range All() {
		if !policy.Permission.Valid() || !policy.ResourceType.Valid() ||
			policy.DefaultDeadline <= 0 || policy.DefaultDeadline > policy.MaximumDeadline ||
			policy.MaximumDeadline > 5*time.Minute || policy.MaximumAttempts == 0 ||
			policy.MaximumAttempts > 3 {
			t.Fatalf("%s has invalid policy: %#v", method, policy)
		}
		if policy.Idempotency != IdempotencyRead && policy.MaximumAttempts != 1 {
			t.Fatalf("%s automatically retries a mutation or stream", method)
		}
	}
}

func TestMutationRequiresExactlyOneValidIdempotencyKey(t *testing.T) {
	info := &grpc.UnaryServerInfo{FullMethod: "/mindclade.orchestration.v1.RunService/CreateRun"}
	handler := func(context.Context, any) (any, error) { return "ok", nil }
	interceptor := UnaryEnforcement()
	if _, err := interceptor(context.Background(), nil, info, handler); !faults.IsReason(err, "idempotency_key_required") {
		t.Fatalf("missing key error = %v", err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("idempotency-key", "request-123456"))
	if result, err := interceptor(ctx, nil, info, handler); err != nil || result != "ok" {
		t.Fatalf("valid key = (%v, %v)", result, err)
	}
}

func TestDeadlineIsDefaultedAndCapped(t *testing.T) {
	info := &grpc.UnaryServerInfo{FullMethod: "/mindclade.artifact.v1.ArtifactService/GetArtifact"}
	interceptor := UnaryEnforcement()
	handler := func(ctx context.Context, _ any) (any, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("deadline was not installed")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > 11*time.Second {
			t.Fatalf("default deadline remaining = %s", remaining)
		}
		return nil, nil
	}
	if _, err := interceptor(context.Background(), nil, info, handler); err != nil {
		t.Fatal(err)
	}

	long, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	handler = func(ctx context.Context, _ any) (any, error) {
		deadline, _ := ctx.Deadline()
		if remaining := time.Until(deadline); remaining > 31*time.Second {
			t.Fatalf("deadline was not capped: %s", remaining)
		}
		return nil, nil
	}
	if _, err := interceptor(long, nil, info, handler); err != nil {
		t.Fatal(err)
	}
}

func TestPublicSurfaceIsExact(t *testing.T) {
	for _, method := range []string{
		"/grpc.health.v1.Health/Check",
		"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo",
	} {
		if !Public(method) {
			t.Fatalf("%s is not public", method)
		}
	}
	if Public("/mindclade.artifact.v1.ArtifactService/GetArtifact") || Public("/unknown.Service/Method") {
		t.Fatal("domain or unknown method was public")
	}
}

func TestReadClientRetriesOnlyDeclaredTransientFailures(t *testing.T) {
	interceptor := unaryClientEnforcement(func(context.Context, time.Duration) error { return nil })
	attempts := 0
	err := interceptor(
		context.Background(),
		"/mindclade.registry.v1.ModelRegistryService/ListModels",
		nil,
		nil,
		nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			attempts++
			if attempts < 3 {
				return status.Error(codes.Unavailable, "transient")
			}
			return nil
		},
	)
	if err != nil || attempts != 3 {
		t.Fatalf("read retry = (%d, %v), want (3, nil)", attempts, err)
	}

	attempts = 0
	err = interceptor(
		context.Background(),
		"/mindclade.registry.v1.ModelRegistryService/ListModels",
		nil,
		nil,
		nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			attempts++
			return status.Error(codes.InvalidArgument, "permanent")
		},
	)
	if status.Code(err) != codes.InvalidArgument || attempts != 1 {
		t.Fatalf("permanent retry = (%d, %v), want one attempt", attempts, err)
	}
}

func TestMutationClientRequiresKeyAndNeverRetries(t *testing.T) {
	interceptor := unaryClientEnforcement(func(context.Context, time.Duration) error {
		return errors.New("mutation attempted to back off")
	})
	invoked := false
	err := interceptor(
		context.Background(),
		"/mindclade.orchestration.v1.RunService/CreateRun",
		nil,
		nil,
		nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			invoked = true
			return nil
		},
	)
	if !faults.IsReason(err, "idempotency_key_required") || invoked {
		t.Fatalf("missing key = (%t, %v)", invoked, err)
	}

	ctx := metadata.NewOutgoingContext(
		context.Background(), metadata.Pairs("idempotency-key", "request-123456"),
	)
	attempts := 0
	err = interceptor(
		ctx,
		"/mindclade.orchestration.v1.RunService/CreateRun",
		nil,
		nil,
		nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			attempts++
			return status.Error(codes.Unavailable, "ambiguous mutation result")
		},
	)
	if status.Code(err) != codes.Unavailable || attempts != 1 {
		t.Fatalf("mutation retry = (%d, %v), want one attempt", attempts, err)
	}
}

func TestClientDeadlineCancellationStopsRetry(t *testing.T) {
	interceptor := unaryClientEnforcement(func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		return context.Cause(ctx)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := interceptor(
		ctx,
		"/mindclade.artifact.v1.ArtifactService/ListArtifacts",
		nil,
		nil,
		nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			return status.Error(codes.Unavailable, "transient")
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retry cancellation = %v", err)
	}
}

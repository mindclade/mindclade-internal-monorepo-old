// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package interceptors

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"connectrpc.com/connect"

	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/connectx"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/requestmeta"
)

func TestEstablishMetadata(t *testing.T) {
	ctx, err := establishMetadata(context.Background(), http.Header{}, "/mindclade.test.v1.TestService/Get")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := requestmeta.RequestIDFromContext(ctx); !ok {
		t.Fatal("request ID missing")
	}
	operation, ok := requestmeta.OperationFromContext(ctx)
	if !ok || operation.String() != "rpc.mindclade.test.v1.TestService.Get" {
		t.Fatalf("operation=%q", operation.String())
	}
}

func TestCredentialFromHeader(t *testing.T) {
	header := http.Header{"Authorization": []string{"Bearer token"}}
	credential, present, err := credentialFromHeader(header, "X-Api-Key")
	if err != nil || !present || credential.Scheme() != auth.CredentialSchemeBearer {
		t.Fatalf("credential=%v present=%v err=%v", credential, present, err)
	}
	header.Set("X-Api-Key", "api-key")
	if _, _, err := credentialFromHeader(header, "X-Api-Key"); err == nil {
		t.Fatal("expected multiple credential error")
	}
}

func TestValidateMessage(t *testing.T) {
	err := validateMessage(&testMessage{invalid: true})
	if faults.CodeOf(err) != faults.CodeInvalidArgument {
		t.Fatalf("err=%v", err)
	}
	if err := validateMessage(struct{}{}); err != nil {
		t.Fatal(err)
	}
}

func TestPanicErrorIsSafe(t *testing.T) {
	err := connectx.DecodeError(panicError(context.Background(), "/mindclade.test.v1.TestService/Panic"))
	if faults.CodeOf(err) != faults.CodeInternal || faults.PublicMessageOf(err) != "internal server error" {
		t.Fatalf("err=%v", err)
	}
}

func TestAuthenticationHelper(t *testing.T) {
	principal, err := auth.NewPrincipal(auth.PrincipalKindService, "gateway", auth.WithIssuer("mindclade.test"))
	if err != nil {
		t.Fatal(err)
	}
	authenticator := auth.AuthenticatorFunc(func(context.Context, auth.Credential) (auth.Principal, error) { return principal, nil })
	interceptor := authenticationInterceptor{config: AuthenticationConfig{Authenticator: authenticator, APIKeyHeader: "X-Api-Key"}}
	ctx, err := interceptor.authenticate(context.Background(), http.Header{"Authorization": []string{"Bearer token"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := auth.PrincipalFromContext(ctx); !ok {
		t.Fatal("principal missing")
	}
}

type testMessage struct{ invalid bool }

func (message *testMessage) Validate() error {
	if message.invalid {
		return errors.New("bad field")
	}
	return nil
}

type markerInterceptor struct{}

func (*markerInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc { return next }
func (*markerInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}
func (*markerInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

func TestStackFiltersNilAndPreservesExtensionPoints(t *testing.T) {
	outer := &markerInterceptor{}
	inner := &markerInterceptor{}
	server := Server(ServerConfig{Outer: []connect.Interceptor{nil, outer}, Inner: []connect.Interceptor{inner, nil}})
	if len(server) != 5 {
		t.Fatalf("server interceptors=%d", len(server))
	}
	client := ClientWithConfig(ClientConfig{Outer: []connect.Interceptor{outer}, Inner: []connect.Interceptor{inner}})
	if len(client) != 4 {
		t.Fatalf("client interceptors=%d", len(client))
	}
}

func TestCredentialFromHeaderRejectsDuplicateAuthorizationValues(t *testing.T) {
	header := http.Header{"Authorization": []string{"Bearer one", "Bearer two"}}
	if _, _, err := credentialFromHeader(header, "X-Api-Key"); faults.CodeOf(err) != faults.CodeUnauthenticated {
		t.Fatalf("code=%s err=%v", faults.CodeOf(err), err)
	}
}

type typedNilConnectAuthorizationResolver struct{}

func (*typedNilConnectAuthorizationResolver) Resolve(context.Context, string) (auth.Permission, auth.Resource, error) {
	panic("typed nil resolver called")
}

func TestAuthorizationTreatsTypedNilResolverAsMissing(t *testing.T) {
	var resolver *typedNilConnectAuthorizationResolver
	err := authorizationInterceptor{config: AuthorizationConfig{Resolver: resolver}}.authorize(context.Background(), "/mindclade.test.v1.TestService/Get")
	if faults.CodeOf(err) != faults.CodeFailedPrecondition {
		t.Fatalf("code=%s err=%v", faults.CodeOf(err), err)
	}
}

func TestPrepareClientMetadataGeneratesLineageAndOperation(t *testing.T) {
	ctx, err := prepareClientMetadata(context.Background(), "/mindclade.test.v1.TestService/Get")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := requestmeta.RequestIDFromContext(ctx); !ok {
		t.Fatal("request ID missing")
	}
	operation, ok := requestmeta.OperationFromContext(ctx)
	if !ok || operation.String() != "rpc.mindclade.test.v1.TestService.Get" {
		t.Fatalf("operation=%q", operation.String())
	}
	carrier := requestmeta.MapCarrier{}
	if err := requestmeta.Inject(ctx, carrier); err != nil {
		t.Fatal(err)
	}
	if carrier.Get(requestmeta.PropagationKeyRequestID) == "" {
		t.Fatal("outbound request ID missing")
	}
}

func TestEstablishMetadataRejectsAmbiguousLineage(t *testing.T) {
	header := http.Header{}
	header.Add(httpx.HeaderRequestID, "request_019c7af21b8276d2a0d522fe41739a21")
	header.Add(httpx.HeaderRequestID, "request_019c7af21b827f53a6b84710f1815c84")
	if _, err := establishMetadata(context.Background(), header, "/mindclade.test.v1.TestService/Get"); faults.ReasonOf(err) != "ambiguous_request_metadata" {
		t.Fatalf("reason=%q err=%v", faults.ReasonOf(err), err)
	}
}

// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package interceptors

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"mindclade.internal/libs/go/auth"
	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/requestmeta"
)

const testFullMethod = "/mindclade.runs.v1.RunService/GetRun"

type fakeServerStream struct {
	ctx     context.Context
	header  metadata.MD
	trailer metadata.MD
	recv    func(any) error
	send    func(any) error
}

func (stream *fakeServerStream) SetHeader(value metadata.MD) error {
	stream.header = metadata.Join(stream.header, value)
	return nil
}
func (stream *fakeServerStream) SendHeader(value metadata.MD) error {
	return stream.SetHeader(value)
}
func (stream *fakeServerStream) SetTrailer(value metadata.MD) {
	stream.trailer = metadata.Join(stream.trailer, value)
}
func (stream *fakeServerStream) Context() context.Context {
	if stream.ctx == nil {
		return context.Background()
	}
	return stream.ctx
}
func (stream *fakeServerStream) SendMsg(message any) error {
	if stream.send != nil {
		return stream.send(message)
	}
	return nil
}
func (stream *fakeServerStream) RecvMsg(message any) error {
	if stream.recv != nil {
		return stream.recv(message)
	}
	return io.EOF
}

type fakeClientStream struct {
	ctx       context.Context
	header    metadata.MD
	trailer   metadata.MD
	headerErr error
	closeErr  error
	sendErr   error
	recvErr   error
}

func (stream *fakeClientStream) Header() (metadata.MD, error) {
	return stream.header.Copy(), stream.headerErr
}
func (stream *fakeClientStream) Trailer() metadata.MD { return stream.trailer.Copy() }
func (stream *fakeClientStream) CloseSend() error     { return stream.closeErr }
func (stream *fakeClientStream) Context() context.Context {
	if stream.ctx == nil {
		return context.Background()
	}
	return stream.ctx
}
func (stream *fakeClientStream) SendMsg(any) error { return stream.sendErr }
func (stream *fakeClientStream) RecvMsg(any) error { return stream.recvErr }

func TestUnaryRequestMetadataEstablishesLineage(t *testing.T) {
	interceptor := UnaryRequestMetadata()
	called := false
	_, err := interceptor(
		context.Background(),
		struct{}{},
		&grpc.UnaryServerInfo{FullMethod: testFullMethod},
		func(ctx context.Context, _ any) (any, error) {
			called = true
			metadataValue, ok := requestmeta.FromContext(ctx)
			if !ok || metadataValue.RequestID.IsZero() {
				t.Fatal("request metadata was not established")
			}
			if got, want := metadataValue.Operation.String(), "rpc.mindclade.runs.v1.RunService.GetRun"; got != want {
				t.Fatalf("operation=%q want=%q", got, want)
			}
			return "ok", nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
}

func TestUnaryClientRequestMetadataInjectsLineage(t *testing.T) {
	interceptor := UnaryClientRequestMetadata()
	called := false
	err := interceptor(
		context.Background(),
		testFullMethod,
		struct{}{},
		&struct{}{},
		nil,
		func(ctx context.Context, method string, request, response any, connection *grpc.ClientConn, options ...grpc.CallOption) error {
			called = true
			outgoing, ok := metadata.FromOutgoingContext(ctx)
			if !ok || len(outgoing.Get(requestmeta.PropagationKeyRequestID)) != 1 {
				t.Fatalf("request ID was not injected: %v", outgoing)
			}
			metadataValue, ok := requestmeta.FromContext(ctx)
			if !ok || metadataValue.Operation.IsZero() {
				t.Fatal("operation was not established")
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("invoker was not called")
	}
}

func TestStreamRequestMetadataReplacesContext(t *testing.T) {
	stream := &fakeServerStream{ctx: context.Background()}
	interceptor := StreamRequestMetadata()
	called := false
	err := interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: testFullMethod}, func(_ any, received grpc.ServerStream) error {
		called = true
		metadataValue, ok := requestmeta.FromContext(received.Context())
		if !ok || metadataValue.RequestID.IsZero() || metadataValue.Operation.IsZero() {
			t.Fatalf("missing metadata: %#v", metadataValue)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
	if len(stream.header.Get(requestmeta.PropagationKeyRequestID)) != 1 {
		t.Fatalf("response header=%v", stream.header)
	}
}

func TestUnaryFaultTranslationRoundTrip(t *testing.T) {
	server := UnaryFaultTranslation()
	_, encoded := server(context.Background(), struct{}{}, &grpc.UnaryServerInfo{FullMethod: testFullMethod}, func(context.Context, any) (any, error) {
		return nil, faults.New(
			faults.CodeNotFound,
			"training run was not found",
			faults.WithReason("run_not_found"),
		)
	})
	if got := status.Code(encoded); got != codes.NotFound {
		t.Fatalf("status code=%s", got)
	}

	client := UnaryClientFaultTranslation()
	decoded := client(context.Background(), testFullMethod, struct{}{}, &struct{}{}, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		return encoded
	})
	if got := faults.CodeOf(decoded); got != faults.CodeNotFound {
		t.Fatalf("fault code=%s err=%v", got, decoded)
	}
	if got := faults.ReasonOf(decoded); got != "run_not_found" {
		t.Fatalf("reason=%q", got)
	}
}

func TestStreamClientFaultTranslationDecodesOperations(t *testing.T) {
	wire := status.Error(codes.Unavailable, "temporarily unavailable")
	base := &fakeClientStream{recvErr: wire, sendErr: wire, closeErr: wire, headerErr: wire}
	interceptor := StreamClientFaultTranslation()
	stream, err := interceptor(context.Background(), &grpc.StreamDesc{}, nil, testFullMethod, func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, ...grpc.CallOption) (grpc.ClientStream, error) {
		return base, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, operation := range map[string]func() error{
		"receive": func() error { return stream.RecvMsg(&struct{}{}) },
		"send":    func() error { return stream.SendMsg(struct{}{}) },
		"close":   stream.CloseSend,
		"header": func() error {
			_, err := stream.Header()
			return err
		},
	} {
		if got := faults.CodeOf(operation()); got != faults.CodeUnavailable {
			t.Fatalf("%s code=%s", name, got)
		}
	}
}

func TestUnaryRecoveryContainsPanic(t *testing.T) {
	var mu sync.Mutex
	var observed PanicEvent
	observer := PanicObserverFunc(func(event PanicEvent) {
		mu.Lock()
		observed = event
		mu.Unlock()
	})
	interceptor := UnaryRecovery(observer)
	_, err := interceptor(context.Background(), struct{}{}, &grpc.UnaryServerInfo{FullMethod: testFullMethod}, func(context.Context, any) (any, error) {
		panic("private panic value")
	})
	if got := faults.CodeOf(err); got != faults.CodeInternal {
		t.Fatalf("code=%s err=%v", got, err)
	}
	if got := faults.ReasonOf(err); got != "rpc_handler_panic" {
		t.Fatalf("reason=%q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if observed.Method != testFullMethod || observed.Value != "private panic value" || len(observed.Stack) == 0 {
		t.Fatalf("event=%#v", observed)
	}
	if errors.Is(err, errors.New("private panic value")) || faults.PublicMessageOf(err) != "internal server error" {
		t.Fatalf("panic leaked: %v", err)
	}
}

type nilPanicObserver struct{}

func (*nilPanicObserver) ObservePanic(PanicEvent) { panic("typed nil observer called") }

func TestRecoveryIgnoresTypedNilAndPanickingObservers(t *testing.T) {
	var typedNil *nilPanicObserver
	observers := []PanicObserver{
		typedNil,
		PanicObserverFunc(func(PanicEvent) { panic("observer panic") }),
	}
	for _, observer := range observers {
		_, err := UnaryRecovery(observer)(context.Background(), struct{}{}, &grpc.UnaryServerInfo{FullMethod: testFullMethod}, func(context.Context, any) (any, error) {
			panic("handler panic")
		})
		if faults.CodeOf(err) != faults.CodeInternal {
			t.Fatalf("code=%s err=%v", faults.CodeOf(err), err)
		}
	}
}

func TestUnaryAuthenticationEstablishesPrincipal(t *testing.T) {
	principal, err := auth.NewPrincipal(auth.PrincipalKindService, "gateway", auth.WithIssuer("mindclade.test"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer token"))
	interceptor := UnaryAuthentication(AuthenticationConfig{
		Authenticator: auth.AuthenticatorFunc(func(_ context.Context, credential auth.Credential) (auth.Principal, error) {
			if credential.Scheme() != auth.CredentialSchemeBearer || string(credential.Value()) != "token" {
				t.Fatalf("credential=%v", credential)
			}
			return principal, nil
		}),
	})
	called := false
	_, err = interceptor(ctx, struct{}{}, &grpc.UnaryServerInfo{FullMethod: testFullMethod}, func(ctx context.Context, _ any) (any, error) {
		called = true
		got, err := auth.RequirePrincipal(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got.Subject() != principal.Subject() {
			t.Fatalf("principal=%#v", got)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
}

type typedNilCredentialExtractor struct{}

func (*typedNilCredentialExtractor) ExtractCredential(metadata.MD) (auth.Credential, bool, error) {
	panic("typed nil extractor called")
}

func TestAuthenticationUsesDefaultForTypedNilExtractor(t *testing.T) {
	var extractor *typedNilCredentialExtractor
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer token"))
	principal, err := auth.NewPrincipal(auth.PrincipalKindService, "gateway", auth.WithIssuer("mindclade.test"))
	if err != nil {
		t.Fatal(err)
	}
	interceptor := UnaryAuthentication(AuthenticationConfig{
		Extractor: extractor,
		Authenticator: auth.AuthenticatorFunc(func(context.Context, auth.Credential) (auth.Principal, error) {
			return principal, nil
		}),
	})
	if _, err := interceptor(ctx, struct{}{}, &grpc.UnaryServerInfo{FullMethod: testFullMethod}, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestUnaryAuthorizationAllowsMappedOperation(t *testing.T) {
	principal, err := auth.NewPrincipal(auth.PrincipalKindService, "gateway", auth.WithIssuer("mindclade.test"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := auth.WithPrincipal(context.Background(), principal)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := auth.NewResource(auth.ResourceType("training_run"))
	if err != nil {
		t.Fatal(err)
	}
	permission := auth.MustParsePermission("runs.read")
	interceptor := UnaryAuthorization(AuthorizationConfig{
		Resolver: AuthorizationResolverFunc(func(method string, request any) (AuthorizationTarget, bool, error) {
			if method != testFullMethod {
				t.Fatalf("method=%q", method)
			}
			return AuthorizationTarget{Permission: permission, Resource: resource}, true, nil
		}),
		Authorizer: auth.AuthorizerFunc(func(_ context.Context, request auth.AuthorizationRequest) (auth.Decision, error) {
			if request.Permission != permission {
				t.Fatalf("permission=%q", request.Permission)
			}
			return auth.Allow("permission_granted"), nil
		}),
	})
	called := false
	_, err = interceptor(ctx, struct{}{}, &grpc.UnaryServerInfo{FullMethod: testFullMethod}, func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
}

type typedNilAuthorizationResolver struct{}

func (*typedNilAuthorizationResolver) ResolveAuthorization(string, any) (AuthorizationTarget, bool, error) {
	panic("typed nil resolver called")
}

func TestAuthorizationFailsClosedForMissingOrTypedNilMapping(t *testing.T) {
	var typedNil *typedNilAuthorizationResolver
	for _, resolver := range []AuthorizationResolver{nil, typedNil} {
		_, err := UnaryAuthorization(AuthorizationConfig{Resolver: resolver, RequireMapping: true})(
			context.Background(), struct{}{}, &grpc.UnaryServerInfo{FullMethod: testFullMethod},
			func(context.Context, any) (any, error) { t.Fatal("handler called"); return nil, nil },
		)
		if got := faults.CodeOf(err); got != faults.CodePermissionDenied {
			t.Fatalf("code=%s err=%v", got, err)
		}
	}
}

func TestStreamValidationValidatesReceivedMessages(t *testing.T) {
	base := &fakeServerStream{recv: func(target any) error {
		value, ok := target.(*message)
		if !ok {
			t.Fatalf("target=%T", target)
		}
		value.invalid = true
		return nil
	}}
	interceptor := StreamValidation()
	err := interceptor(nil, base, &grpc.StreamServerInfo{FullMethod: testFullMethod}, func(_ any, stream grpc.ServerStream) error {
		var value message
		return stream.RecvMsg(&value)
	})
	if got := faults.CodeOf(err); got != faults.CodeInvalidArgument {
		t.Fatalf("code=%s err=%v", got, err)
	}
}

func TestInterceptorStacksFilterNilEntries(t *testing.T) {
	unaryServer, streamServer := Server(ServerConfig{
		ValidateMessages: true,
		AdditionalUnary: []grpc.UnaryServerInterceptor{nil, func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return handler(ctx, req)
		}},
		AdditionalStream: []grpc.StreamServerInterceptor{nil, func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			return handler(srv, stream)
		}},
	})
	if len(unaryServer) != 5 || len(streamServer) != 5 {
		t.Fatalf("server lengths=(%d,%d)", len(unaryServer), len(streamServer))
	}
	unaryClient, streamClient := Client([]grpc.UnaryClientInterceptor{nil, func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
		return invoker(ctx, method, req, reply, cc, options...)
	}}, []grpc.StreamClientInterceptor{nil})
	if len(unaryClient) != 3 || len(streamClient) != 2 {
		t.Fatalf("client lengths=(%d,%d)", len(unaryClient), len(streamClient))
	}
}

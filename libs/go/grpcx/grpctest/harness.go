// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package grpctest

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"net"
	"testing"
)

type Harness struct {
	Listener *bufconn.Listener
	Server   *grpc.Server
}

func Start(testingTB testing.TB, server *grpc.Server) *Harness {
	testingTB.Helper()
	listener := bufconn.Listen(1 << 20)
	harness := &Harness{Listener: listener, Server: server}
	go func() { _ = server.Serve(listener) }()
	testingTB.Cleanup(func() { server.Stop(); _ = listener.Close() })
	return harness
}
func (harness *Harness) Client(ctx context.Context, options ...grpc.DialOption) (*grpc.ClientConn, error) {
	dialer := func(context.Context, string) (net.Conn, error) { return harness.Listener.Dial() }
	base := []grpc.DialOption{grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials())}
	return grpc.NewClient("passthrough:///bufnet", append(base, options...)...)
}

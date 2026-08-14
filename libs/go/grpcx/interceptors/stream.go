// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package interceptors

import (
	"context"
	"google.golang.org/grpc"
)

type serverStreamWithContext struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *serverStreamWithContext) Context() context.Context { return stream.ctx }

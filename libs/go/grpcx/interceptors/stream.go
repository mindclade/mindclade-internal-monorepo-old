// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

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

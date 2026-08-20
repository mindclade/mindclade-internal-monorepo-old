// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package grpcx

import (
	"context"
	"strings"

	"google.golang.org/grpc/metadata"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/requestmeta"
)

type MetadataCarrier struct {
	Metadata metadata.MD
}

func (carrier MetadataCarrier) Get(key string) string {
	if carrier.Metadata == nil {
		return ""
	}
	values := carrier.Metadata.Get(strings.ToLower(key))
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (carrier MetadataCarrier) Set(key, value string) {
	if carrier.Metadata != nil {
		carrier.Metadata.Set(strings.ToLower(key), value)
	}
}

func ExtractIncoming(ctx context.Context) (context.Context, requestmeta.RequestID, error) {
	if ctx == nil {
		return nil, requestmeta.RequestID{}, faults.New(
			faults.CodeInvalidArgument,
			"gRPC context is required",
			faults.WithReason("nil_context"),
			faults.WithOperation("grpcx.ExtractIncoming"),
		)
	}
	incoming, _ := metadata.FromIncomingContext(ctx)
	if err := validateLineageMetadata(incoming); err != nil {
		return ctx, requestmeta.RequestID{}, err
	}
	return requestmeta.ExtractOrGenerate(ctx, MetadataCarrier{Metadata: incoming})
}

func InjectOutgoing(ctx context.Context) (context.Context, error) {
	if ctx == nil {
		return nil, faults.New(
			faults.CodeInvalidArgument,
			"gRPC context is required",
			faults.WithReason("nil_context"),
			faults.WithOperation("grpcx.InjectOutgoing"),
		)
	}
	var err error
	ctx, _, err = requestmeta.EnsureRequestID(ctx)
	if err != nil {
		return ctx, err
	}
	outgoing, _ := metadata.FromOutgoingContext(ctx)
	outgoing = outgoing.Copy()
	if outgoing == nil {
		outgoing = metadata.MD{}
	}
	if err := requestmeta.Inject(ctx, MetadataCarrier{Metadata: outgoing}); err != nil {
		return ctx, err
	}
	return metadata.NewOutgoingContext(ctx, outgoing), nil
}

func validateLineageMetadata(value metadata.MD) error {
	for _, key := range []string{
		requestmeta.PropagationKeyRequestID,
		requestmeta.PropagationKeyCorrelationID,
		requestmeta.PropagationKeyCausationID,
	} {
		nonEmpty := 0
		for _, candidate := range value.Get(key) {
			if strings.TrimSpace(candidate) != "" {
				nonEmpty++
			}
		}
		if nonEmpty > 1 {
			return faults.New(
				faults.CodeInvalidArgument,
				"ambiguous request metadata",
				faults.WithReason("ambiguous_request_metadata"),
				faults.WithOperation("grpcx.ExtractIncoming"),
				faults.WithField("metadata_key", key),
			)
		}
	}
	return nil
}

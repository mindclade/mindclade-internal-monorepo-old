// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package transport

import (
	"context"
	"net/http"
	"strings"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/httpx"
	"go.mindclade.dev/libs/go/httpx/middleware"
	"go.mindclade.dev/libs/go/resourceversion"
)

type preconditionKey struct{}

// Preconditions translates HTTP conditional-request headers into the canonical
// optimistic-concurrency contract once, at the transport boundary, so no
// handler parses ETags itself and no mutation silently ignores an If-Match a
// client sent.
//
//	If-Match: "<etag>"  -> resourceversion.MatchVersion
//	If-Match: *         -> resourceversion.RequireExistence
//	If-None-Match: *    -> resourceversion.RequireAbsence
//
// A malformed header is rejected here rather than downgraded to an
// unconditional write.
func Preconditions() middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			precondition, present, err := decodePrecondition(request)
			if err != nil {
				httpx.WriteError(request.Context(), writer, err, request.URL.Path)
				return
			}
			if !present {
				next.ServeHTTP(writer, request)
				return
			}
			next.ServeHTTP(writer, request.WithContext(
				context.WithValue(request.Context(), preconditionKey{}, precondition),
			))
		})
	}
}

// PreconditionFrom returns the precondition decoded for this request, if any.
func PreconditionFrom(ctx context.Context) (resourceversion.Precondition, bool) {
	if ctx == nil {
		return resourceversion.Precondition{}, false
	}
	precondition, ok := ctx.Value(preconditionKey{}).(resourceversion.Precondition)
	return precondition, ok
}

func decodePrecondition(request *http.Request) (resourceversion.Precondition, bool, error) {
	match := strings.TrimSpace(request.Header.Get("If-Match"))
	noneMatch := strings.TrimSpace(request.Header.Get("If-None-Match"))
	if match != "" && noneMatch != "" {
		return resourceversion.Precondition{}, false, invalidPrecondition("conflicting_preconditions")
	}
	switch {
	case match == "" && noneMatch == "":
		return resourceversion.Precondition{}, false, nil
	case match == "*":
		return resourceversion.RequireExistence(), true, nil
	case noneMatch == "*":
		return resourceversion.RequireAbsence(), true, nil
	case noneMatch != "":
		return resourceversion.Precondition{}, false, invalidPrecondition("unsupported_if_none_match")
	}
	version, err := resourceversion.ParseETag(match)
	if err != nil {
		return resourceversion.Precondition{}, false, faults.Wrap(err, faults.CodeInvalidArgument,
			"invalid If-Match precondition",
			faults.WithReason("invalid_if_match"),
			faults.WithOperation("controlplane.transport.decodePrecondition"),
			faults.WithRetryPolicy(faults.NoRetry()),
		)
	}
	precondition := resourceversion.MatchVersion(version)
	if err := precondition.Validate(); err != nil {
		return resourceversion.Precondition{}, false, err
	}
	return precondition, true, nil
}

func invalidPrecondition(reason string) error {
	return faults.New(
		faults.CodeInvalidArgument,
		"invalid conditional request",
		faults.WithReason(reason),
		faults.WithOperation("controlplane.transport.decodePrecondition"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

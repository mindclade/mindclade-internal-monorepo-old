// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpx

import (
	"net/http"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/requestmeta"
)

// PrepareRequest returns a shallow request clone with an independent header
// map, a guaranteed request identifier, and canonical outbound lineage
// headers. The caller's request is never mutated.
func PrepareRequest(request *http.Request) (*http.Request, error) {
	if request == nil {
		return nil, faults.New(
			faults.CodeInvalidArgument,
			"HTTP request is required",
			faults.WithReason("nil_http_request"),
			faults.WithOperation("httpx.PrepareRequest"),
		)
	}
	ctx, _, err := requestmeta.EnsureRequestID(request.Context())
	if err != nil {
		return nil, err
	}
	prepared := request.Clone(ctx)
	prepared.Header = request.Header.Clone()
	if prepared.Header == nil {
		prepared.Header = make(http.Header)
	}
	if err := requestmeta.Inject(ctx, HeaderCarrier{Header: prepared.Header}); err != nil {
		return nil, err
	}
	return prepared, nil
}

// RequestMetadataTransport guarantees outbound request lineage before
// delegating to Base. A nil Base uses http.DefaultTransport without mutating it.
type RequestMetadataTransport struct {
	Base http.RoundTripper
}

func (transport RequestMetadataTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	prepared, err := PrepareRequest(request)
	if err != nil {
		return nil, err
	}
	base := transport.Base
	if nilValue(base) {
		base = http.DefaultTransport
	}
	return base.RoundTrip(prepared)
}

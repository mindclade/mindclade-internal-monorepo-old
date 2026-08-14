// Copyright 2026 Mindclade. All rights reserved.
package pagination

import "mindclade.internal/libs/go/faults"

const (
	DefaultPageSize = 50
	MaximumPageSize = 1000
)

type Request struct {
	PageSize int
	Token    string
}

func (request Request) Normalized() Request {
	if request.PageSize == 0 {
		request.PageSize = DefaultPageSize
	}
	return request
}
func (request Request) Validate() error {
	request = request.Normalized()
	if request.PageSize < 1 || request.PageSize > MaximumPageSize {
		return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "invalid page size", faults.WithReason("invalid_page_size"), faults.WithOperation("pagination.Request.Validate"), faults.WithField("page_size", request.PageSize), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if len(request.Token) > MaximumTokenBytes {
		return faults.Wrap(ErrInvalidRequest, faults.CodeInvalidArgument, "page token is too large", faults.WithReason("page_token_too_large"), faults.WithOperation("pagination.Request.Validate"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return nil
}

type Page[T any] struct {
	Items     []T
	NextToken string
}

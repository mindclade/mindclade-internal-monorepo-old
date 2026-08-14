// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/internal/rpcfaults"
)

const (
	ProblemMediaType    = "application/problem+json"
	MaximumProblemBytes = 1 << 20
)

// Problem is Mindclade's RFC 9457-compatible HTTP error envelope. Extension
// members are deliberately bounded and explicit.
type Problem struct {
	Type              string `json:"type,omitempty"`
	Title             string `json:"title"`
	Status            int    `json:"status"`
	Detail            string `json:"detail,omitempty"`
	Instance          string `json:"instance,omitempty"`
	Code              string `json:"code"`
	Reason            string `json:"reason,omitempty"`
	Operation         string `json:"operation,omitempty"`
	RequestID         string `json:"request_id,omitempty"`
	ResourceType      string `json:"resource_type,omitempty"`
	ResourceID        string `json:"resource_id,omitempty"`
	Retryable         bool   `json:"retryable,omitempty"`
	RetryAfterSeconds int64  `json:"retry_after_seconds,omitempty"`
}

func (problem Problem) Validate() error {
	if problem.Status < 400 || problem.Status > 599 {
		return ErrInvalidResponse
	}
	code, err := faults.ParseCode(problem.Code)
	if err != nil || StatusFromCode(code) != problem.Status {
		return ErrInvalidResponse
	}
	if strings.TrimSpace(problem.Title) == "" {
		return ErrInvalidResponse
	}
	if problem.RetryAfterSeconds < 0 {
		return ErrInvalidResponse
	}
	return nil
}

// ProblemFromError creates a client-safe problem. instance should normally be
// the URL path only; query parameters may contain credentials or private data.
func ProblemFromError(ctx context.Context, err error, instance string) Problem {
	details := rpcfaults.FromError(ctx, err)
	status := StatusFromCode(details.Code)
	retryAfter := details.RetryAfter()
	var retrySeconds int64
	if retryAfter > 0 {
		retrySeconds = int64(math.Ceil(retryAfter.Seconds()))
	}
	return Problem{
		Type:              "about:blank",
		Title:             titleForStatus(status),
		Status:            status,
		Detail:            details.Message,
		Instance:          cleanInstance(instance),
		Code:              details.Code.String(),
		Reason:            details.Reason,
		Operation:         details.Operation,
		RequestID:         details.RequestID,
		ResourceType:      details.Resource.Type,
		ResourceID:        details.Resource.ID,
		Retryable:         details.Retry.Retryable(),
		RetryAfterSeconds: retrySeconds,
	}
}

// Error reconstructs a local transport-neutral fault without inventing a
// server-side cause. Inconsistent status/code combinations are reduced to the
// canonical code for the HTTP status.
func (problem Problem) Error() error {
	status := problem.Status
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	code := CodeFromStatus(status)
	if parsed, err := faults.ParseCode(problem.Code); err == nil && StatusFromCode(parsed) == status {
		code = parsed
	}
	message := strings.TrimSpace(problem.Detail)
	if message == "" {
		message = http.StatusText(status)
		if message == "" {
			message = "request failed"
		}
	}
	policy := faults.NoRetry()
	if problem.RetryAfterSeconds > 0 {
		policy = faults.DelayedRetry(time.Duration(problem.RetryAfterSeconds)*time.Second, 0)
	} else if problem.Retryable {
		policy = faults.BackoffRetry(0)
	}
	return rpcfaults.ToError(rpcfaults.Details{
		Code:      code,
		Message:   message,
		Reason:    problem.Reason,
		Operation: problem.Operation,
		RequestID: problem.RequestID,
		Resource: rpcfaults.Resource{
			Type: problem.ResourceType,
			ID:   problem.ResourceID,
		},
		Retry: policy,
	})
}

// WriteError writes a deterministic client-safe problem response.
func WriteError(ctx context.Context, writer http.ResponseWriter, err error, instance string) {
	if writer == nil {
		return
	}
	if err == nil {
		err = faults.New(faults.CodeInternal, "internal server error")
	}
	problem := ProblemFromError(ctx, err, instance)
	payload, marshalErr := json.Marshal(problem)
	if marshalErr != nil {
		problem = ProblemFromError(ctx, faults.New(faults.CodeInternal, "internal server error"), instance)
		payload, _ = json.Marshal(problem)
	}
	payload = append(payload, '\n')
	writer.Header().Set("Content-Type", ProblemMediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if problem.RequestID != "" {
		writer.Header().Set(HeaderRequestID, problem.RequestID)
	}
	if problem.RetryAfterSeconds > 0 {
		writer.Header().Set("Retry-After", strconv.FormatInt(problem.RetryAfterSeconds, 10))
	}
	writer.WriteHeader(problem.Status)
	_, _ = writer.Write(payload)
}

// DecodeError decodes a non-success HTTP response into a local fault. The
// response body is consumed but remains caller-owned and is not closed here.
func DecodeError(response *http.Response) error {
	if response == nil {
		return faults.Wrap(ErrInvalidResponse, faults.CodeInternal, "invalid HTTP response", faults.WithReason("invalid_http_response"))
	}
	if response.StatusCode >= 200 && response.StatusCode < 400 {
		return nil
	}

	fallback := Problem{
		Type:      "about:blank",
		Title:     titleForStatus(response.StatusCode),
		Status:    response.StatusCode,
		Detail:    http.StatusText(response.StatusCode),
		Code:      CodeFromStatus(response.StatusCode).String(),
		RequestID: response.Header.Get(HeaderRequestID),
	}
	if response.Body == nil {
		return fallback.Error()
	}

	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaType != ProblemMediaType && mediaType != "application/json" && mediaType != "" {
		return fallback.Error()
	}

	limited := &io.LimitedReader{R: response.Body, N: MaximumProblemBytes + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	var problem Problem
	if err := decoder.Decode(&problem); err != nil || limited.N <= 0 {
		return fallback.Error()
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || limited.N <= 0 {
		return fallback.Error()
	}
	if problem.Status == 0 {
		problem.Status = response.StatusCode
	}
	if problem.Status != response.StatusCode {
		return fallback.Error()
	}
	if problem.RequestID == "" {
		problem.RequestID = response.Header.Get(HeaderRequestID)
	}
	if problem.Code == "" {
		problem.Code = CodeFromStatus(problem.Status).String()
	}
	if problem.Title == "" {
		problem.Title = titleForStatus(problem.Status)
	}
	if problem.Detail == "" {
		problem.Detail = http.StatusText(problem.Status)
	}
	if err := problem.Validate(); err != nil {
		return fallback.Error()
	}
	return problem.Error()
}

func cleanInstance(instance string) string {
	if before, _, ok := strings.Cut(instance, "?"); ok {
		instance = before
	}
	if len(instance) > 1024 {
		instance = instance[:1024]
	}
	return instance
}

func titleForStatus(status int) string {
	title := http.StatusText(status)
	if title == "" {
		return "Request Failed"
	}
	return title
}

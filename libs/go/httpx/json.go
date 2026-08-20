// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"go.mindclade.dev/libs/go/faults"
)

const DefaultMaximumJSONBytes int64 = 1 << 20

// WriteJSON writes one JSON value after encoding it completely. Encoding is
// performed before response headers are committed so callers never receive a
// partially encoded success response.
func WriteJSON(writer http.ResponseWriter, status int, value any) error {
	if writer == nil {
		return faults.Wrap(ErrInvalidResponse, faults.CodeInternal, "invalid HTTP response writer", faults.WithReason("nil_response_writer"))
	}
	if status < 100 || status > 599 {
		return faults.Wrap(ErrInvalidResponse, faults.CodeInternal, "invalid HTTP response status", faults.WithReason("invalid_response_status"))
	}

	var payload []byte
	if responseBodyAllowed(status) && value != nil {
		encoded, err := json.Marshal(value)
		if err != nil {
			return faults.Wrap(err, faults.CodeInternal, "unable to encode HTTP response", faults.WithReason("response_encoding_failed"))
		}
		payload = append(encoded, '\n')
	}

	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if len(payload) > 0 {
		writer.Header().Set("Content-Type", "application/json")
	}
	writer.WriteHeader(status)
	if len(payload) == 0 {
		return nil
	}
	written, err := writer.Write(payload)
	if err == nil && written != len(payload) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return faults.Wrap(err, faults.CodeUnavailable, "unable to write HTTP response", faults.WithReason("response_write_failed"), faults.WithRetryPolicy(faults.BackoffRetry(0)))
	}
	return nil
}

func responseBodyAllowed(status int) bool {
	return status >= 200 && status != http.StatusNoContent && status != http.StatusNotModified
}

// DecodeJSON decodes exactly one JSON value from request.Body with a strict
// total byte limit and unknown-field rejection.
func DecodeJSON(request *http.Request, destination any, maximumBytes int64) error {
	if request == nil || request.Body == nil || destination == nil {
		return faults.Wrap(ErrInvalidResponse, faults.CodeInvalidArgument, "invalid JSON request", faults.WithReason("invalid_json_request"))
	}
	if maximumBytes <= 0 {
		maximumBytes = DefaultMaximumJSONBytes
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return faults.Wrap(ErrUnsupportedMediaType, faults.CodeInvalidArgument, "content type must be application/json", faults.WithReason("unsupported_media_type"))
	}

	limited := &io.LimitedReader{R: request.Body, N: maximumBytes + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if requestTooLarge(err, limited) {
			return requestTooLargeFault()
		}
		return faults.Wrap(err, faults.CodeInvalidArgument, "invalid JSON request", faults.WithReason("invalid_json"))
	}

	var trailing any
	err = decoder.Decode(&trailing)
	if requestTooLarge(err, limited) {
		return requestTooLargeFault()
	}
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err == nil:
		return faults.New(faults.CodeInvalidArgument, "request must contain one JSON value", faults.WithReason("multiple_json_values"))
	default:
		return faults.Wrap(err, faults.CodeInvalidArgument, "invalid JSON request", faults.WithReason("invalid_json"))
	}
}

func requestTooLarge(err error, limited *io.LimitedReader) bool {
	var maximum *http.MaxBytesError
	return errors.As(err, &maximum) || (limited != nil && limited.N <= 0)
}

func requestTooLargeFault() error {
	return faults.Wrap(
		ErrRequestTooLarge,
		faults.CodeResourceExhausted,
		"request body is too large",
		faults.WithReason("request_body_too_large"),
	)
}

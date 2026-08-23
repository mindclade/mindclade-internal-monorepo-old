// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

// Package gcshttp provides the create-only Cloud Storage transport used by the
// Bazel cache gateway. It intentionally implements only Put, Open, and Stat:
// cache writers cannot update or delete an existing object through this API.
package gcshttp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/blob"
)

const (
	defaultEndpoint       = "https://storage.googleapis.com"
	digestMetadataKey     = "mindclade-sha256"
	metadataHeaderPrefix  = "x-goog-meta-"
	maximumErrorBodyBytes = 64 << 10
	maximumAttempts       = 3
)

var retryDelays = [...]time.Duration{200 * time.Millisecond, 500 * time.Millisecond}

type Store struct {
	client             *http.Client
	bucket             string
	prefix             string
	maximumObjectBytes int64
	endpoint           *url.URL
}

type readSeekAt interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

func New(client *http.Client, bucket, prefix string, maximumObjectBytes int64) (*Store, error) {
	endpoint, err := url.Parse(defaultEndpoint)
	if err != nil {
		return nil, err
	}
	return newStore(client, bucket, prefix, maximumObjectBytes, endpoint)
}

func newStore(client *http.Client, bucket, prefix string, maximumObjectBytes int64, endpoint *url.URL) (*Store, error) {
	const operation = "cache_gateway.gcshttp.New"
	if client == nil || endpoint == nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, invalidConfiguration(operation)
	}
	if strings.TrimSpace(bucket) != bucket || !validBucket(bucket) {
		return nil, invalidConfiguration(operation)
	}
	if strings.TrimSpace(prefix) != prefix || strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/") {
		return nil, invalidConfiguration(operation)
	}
	if _, err := blob.ParseKey(prefix); err != nil || maximumObjectBytes <= 0 {
		return nil, invalidConfiguration(operation)
	}
	return &Store{
		client:             client,
		bucket:             bucket,
		prefix:             prefix,
		maximumObjectBytes: maximumObjectBytes,
		endpoint:           cloneURL(endpoint),
	}, nil
}

func (store *Store) Put(ctx context.Context, key blob.Key, reader io.Reader, options blob.PutOptions) (blob.Attributes, error) {
	const operation = "cache_gateway.gcshttp.Put"
	if ctx == nil || store == nil || store.client == nil || reader == nil {
		return blob.Attributes{}, invalidRequest(operation, "invalid_cache_put_request")
	}
	if err := key.Validate(); err != nil {
		return blob.Attributes{}, err
	}
	if err := options.Validate(); err != nil {
		return blob.Attributes{}, err
	}
	if !options.Preconditions.IfNotExists || options.Preconditions.IfGenerationMatch != nil || !options.Digest.Valid() {
		return blob.Attributes{}, invalidRequest(operation, "create_only_put_required")
	}
	for metadataKey := range options.Metadata {
		if strings.EqualFold(metadataKey, digestMetadataKey) {
			return blob.Attributes{}, invalidRequest(operation, "reserved_cache_metadata")
		}
	}
	seeker, ok := reader.(readSeekAt)
	if !ok {
		return blob.Attributes{}, invalidRequest(operation, "random_access_cache_body_required")
	}
	offset, size, checksum, digest, err := inspectBody(seeker, store.maximumObjectBytes)
	if err != nil {
		return blob.Attributes{}, err
	}
	if !digest.Equal(options.Digest) {
		return blob.Attributes{}, dataLoss(operation, "cache_upload_digest_mismatch", nil)
	}

	headers := make(http.Header, len(options.Metadata)+5)
	headers.Set("Content-Type", options.ContentType)
	headers.Set("X-Goog-If-Generation-Match", "0")
	headers.Set("X-Goog-Content-Length-Range", "0,"+strconv.FormatInt(store.maximumObjectBytes, 10))
	headers.Set("X-Goog-Hash", "crc32c="+checksum)
	headers.Set(metadataHeaderPrefix+digestMetadataKey, options.Digest.String())
	for metadataKey, value := range options.Metadata {
		headers.Set(metadataHeaderPrefix+metadataKey, value)
	}

	response, err := store.do(ctx, http.MethodPut, store.objectURL(key, 0), headers, func() (io.ReadCloser, error) {
		return io.NopCloser(io.NewSectionReader(seeker, offset, size)), nil
	}, size, true)
	if err != nil {
		return blob.Attributes{}, err
	}
	drainAndClose(response)

	attributes, err := store.Stat(ctx, key)
	if err != nil {
		return blob.Attributes{}, err
	}
	if attributes.Size != size || !attributes.Digest.Equal(options.Digest) {
		return blob.Attributes{}, dataLoss(operation, "stored_cache_integrity_mismatch", nil)
	}
	return attributes, nil
}

func (store *Store) Open(ctx context.Context, key blob.Key, options blob.GetOptions) (blob.Object, error) {
	const operation = "cache_gateway.gcshttp.Open"
	if ctx == nil || store == nil || store.client == nil {
		return blob.Object{}, invalidRequest(operation, "invalid_cache_open_request")
	}
	if err := key.Validate(); err != nil {
		return blob.Object{}, err
	}
	if err := options.Validate(); err != nil {
		return blob.Object{}, err
	}
	if options.Offset != 0 || options.Length != 0 {
		return blob.Object{}, invalidRequest(operation, "range_cache_read_unsupported")
	}

	attributes, err := store.stat(ctx, key, options.Generation)
	if err != nil {
		return blob.Object{}, err
	}
	response, err := store.do(ctx, http.MethodGet, store.objectURL(key, attributes.Generation), nil, nil, 0, false)
	if err != nil {
		return blob.Object{}, err
	}
	responseAttributes, err := store.attributesFromHeaders(key, response.Header, response.ContentLength)
	if err != nil {
		_ = response.Body.Close()
		return blob.Object{}, err
	}
	if responseAttributes.Generation != attributes.Generation || responseAttributes.Size != attributes.Size || !responseAttributes.Digest.Equal(attributes.Digest) {
		_ = response.Body.Close()
		return blob.Object{}, dataLoss(operation, "cache_generation_changed", nil)
	}
	return blob.Object{
		Attributes: attributes,
		Body: &verifyingReadCloser{
			body:     response.Body,
			digest:   attributes.Digest,
			expected: attributes.Size,
			hash:     sha256.New(),
		},
	}, nil
}

func (store *Store) Stat(ctx context.Context, key blob.Key) (blob.Attributes, error) {
	return store.stat(ctx, key, nil)
}

func (store *Store) stat(ctx context.Context, key blob.Key, generation *int64) (blob.Attributes, error) {
	const operation = "cache_gateway.gcshttp.Stat"
	if ctx == nil || store == nil || store.client == nil {
		return blob.Attributes{}, invalidRequest(operation, "invalid_cache_stat_request")
	}
	if err := key.Validate(); err != nil {
		return blob.Attributes{}, err
	}
	var pinned int64
	if generation != nil {
		if *generation <= 0 {
			return blob.Attributes{}, invalidRequest(operation, "invalid_cache_generation")
		}
		pinned = *generation
	}
	response, err := store.do(ctx, http.MethodHead, store.objectURL(key, pinned), nil, nil, 0, false)
	if err != nil {
		return blob.Attributes{}, err
	}
	defer response.Body.Close()
	attributes, err := store.attributesFromHeaders(key, response.Header, response.ContentLength)
	if err != nil {
		return blob.Attributes{}, err
	}
	if pinned > 0 && attributes.Generation != pinned {
		return blob.Attributes{}, dataLoss(operation, "cache_generation_mismatch", nil)
	}
	return attributes, nil
}

func inspectBody(reader io.ReadSeeker, maximumBytes int64) (int64, int64, string, identifiers.Digest, error) {
	const operation = "cache_gateway.gcshttp.inspectBody"
	offset, err := reader.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, 0, "", identifiers.Digest{}, faults.Wrap(err, faults.CodeInvalidArgument, "cache body is not seekable", faults.WithReason("cache_body_seek_failed"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	checksum := crc32.New(crc32.MakeTable(crc32.Castagnoli))
	digestHash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(checksum, digestHash), io.LimitReader(reader, maximumBytes+1))
	_, seekErr := reader.Seek(offset, io.SeekStart)
	if copyErr != nil {
		return 0, 0, "", identifiers.Digest{}, faults.Wrap(copyErr, faults.CodeUnavailable, "cache body inspection failed", faults.WithReason("cache_body_read_failed"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.BackoffRetry(maximumAttempts)))
	}
	if seekErr != nil {
		return 0, 0, "", identifiers.Digest{}, faults.Wrap(seekErr, faults.CodeInvalidArgument, "cache body is not seekable", faults.WithReason("cache_body_seek_failed"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if size > maximumBytes {
		return 0, 0, "", identifiers.Digest{}, faults.New(faults.CodeResourceExhausted, "cache object is too large", faults.WithReason("cache_object_too_large"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	digest, err := identifiers.DigestFromBytes(digestHash.Sum(nil))
	if err != nil {
		return 0, 0, "", identifiers.Digest{}, dataLoss(operation, "cache_upload_digest_failed", err)
	}
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], checksum.Sum32())
	return offset, size, base64.StdEncoding.EncodeToString(encoded[:]), digest, nil
}

func (store *Store) do(ctx context.Context, method, requestURL string, headers http.Header, body func() (io.ReadCloser, error), contentLength int64, createOnly bool) (*http.Response, error) {
	const operation = "cache_gateway.gcshttp.request"
	for attempt := 0; attempt < maximumAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, transportFault(operation, ctx.Err())
			case <-time.After(retryDelays[attempt-1]):
			}
		}
		var requestBody io.ReadCloser
		if body != nil {
			var err error
			requestBody, err = body()
			if err != nil {
				return nil, transportFault(operation, err)
			}
		}
		request, err := http.NewRequestWithContext(ctx, method, requestURL, requestBody)
		if err != nil {
			if requestBody != nil {
				_ = requestBody.Close()
			}
			return nil, invalidRequest(operation, "invalid_cache_backend_request")
		}
		request.Header = headers.Clone()
		if body != nil {
			request.ContentLength = contentLength
		}
		response, err := store.client.Do(request)
		if err != nil {
			if attempt+1 < maximumAttempts && ctx.Err() == nil {
				continue
			}
			return nil, transportFault(operation, err)
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}
		if response.StatusCode == http.StatusPreconditionFailed && createOnly {
			drainAndClose(response)
			return nil, faults.New(faults.CodeAlreadyExists, "cache object already exists", faults.WithReason("immutable_cache_object_exists"), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
		}
		if retryableStatus(response.StatusCode) && attempt+1 < maximumAttempts {
			drainAndClose(response)
			continue
		}
		status := response.StatusCode
		drainAndClose(response)
		return nil, statusFault(operation, status, ctx)
	}
	return nil, faults.New(faults.CodeUnavailable, "cache backend unavailable", faults.WithReason("cache_backend_retry_exhausted"), faults.WithOperation(operation), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.BackoffRetry(maximumAttempts)))
}

func (store *Store) attributesFromHeaders(key blob.Key, headers http.Header, contentLength int64) (blob.Attributes, error) {
	const operation = "cache_gateway.gcshttp.attributesFromHeaders"
	if contentLength < 0 || contentLength > store.maximumObjectBytes {
		return blob.Attributes{}, dataLoss(operation, "invalid_cache_object_size", nil)
	}
	generation, err := strconv.ParseInt(headers.Get("X-Goog-Generation"), 10, 64)
	if err != nil || generation <= 0 {
		return blob.Attributes{}, dataLoss(operation, "invalid_cache_object_generation", err)
	}
	digest, err := identifiers.ParseDigest(headers.Get(metadataHeaderPrefix + digestMetadataKey))
	if err != nil {
		return blob.Attributes{}, dataLoss(operation, "invalid_cache_object_digest", err)
	}
	metadata := make(blob.Metadata)
	for name, values := range headers {
		lowerName := strings.ToLower(name)
		if !strings.HasPrefix(lowerName, metadataHeaderPrefix) {
			continue
		}
		metadataKey := strings.TrimPrefix(lowerName, metadataHeaderPrefix)
		if metadataKey == digestMetadataKey {
			continue
		}
		if len(values) != 1 {
			return blob.Attributes{}, dataLoss(operation, "invalid_cache_object_metadata", nil)
		}
		metadata[metadataKey] = values[0]
	}
	attributes := blob.Attributes{
		Key:         key,
		Size:        contentLength,
		Digest:      digest,
		ContentType: headers.Get("Content-Type"),
		ETag:        headers.Get("ETag"),
		Generation:  generation,
		Metadata:    metadata,
	}
	if modified := headers.Get("Last-Modified"); modified != "" {
		if attributes.UpdatedAt, err = http.ParseTime(modified); err != nil {
			return blob.Attributes{}, dataLoss(operation, "invalid_cache_object_timestamp", err)
		}
	}
	if err := attributes.Validate(); err != nil {
		return blob.Attributes{}, dataLoss(operation, "invalid_cache_object_attributes", err)
	}
	return attributes, nil
}

func (store *Store) objectURL(key blob.Key, generation int64) string {
	value := cloneURL(store.endpoint)
	objectName := store.prefix + "/" + key.String()
	value.Path = strings.TrimSuffix(value.Path, "/") + "/" + store.bucket + "/" + objectName
	if generation > 0 {
		query := value.Query()
		query.Set("generation", strconv.FormatInt(generation, 10))
		value.RawQuery = query.Encode()
	}
	return value.String()
}

type verifyingReadCloser struct {
	body     io.ReadCloser
	digest   identifiers.Digest
	expected int64
	read     int64
	hash     hash.Hash
	verified bool
}

func (reader *verifyingReadCloser) Read(buffer []byte) (int, error) {
	count, err := reader.body.Read(buffer)
	if count > 0 {
		reader.read += int64(count)
		_, _ = reader.hash.Write(buffer[:count])
	}
	if errors.Is(err, io.EOF) && !reader.verified {
		reader.verified = true
		actual, digestErr := identifiers.DigestFromBytes(reader.hash.Sum(nil))
		if digestErr != nil || reader.read != reader.expected || !actual.Equal(reader.digest) {
			return count, dataLoss("cache_gateway.gcshttp.Read", "cache_download_integrity_mismatch", digestErr)
		}
	}
	return count, err
}

func (reader *verifyingReadCloser) Close() error {
	return reader.body.Close()
}

func validBucket(value string) bool {
	if len(value) < 3 || len(value) > 63 || !asciiLowerOrDigit(value[0]) {
		return false
	}
	if !asciiLowerOrDigit(value[len(value)-1]) {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '_' && character != '.' {
			return false
		}
	}
	return !strings.Contains(value, "..")
}

func asciiLowerOrDigit(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func statusFault(operation string, status int, ctx context.Context) error {
	code := faults.CodeUnavailable
	reason := "cache_backend_http_error"
	retry := faults.BackoffRetry(maximumAttempts)
	switch status {
	case http.StatusUnauthorized:
		code, reason, retry = faults.CodeUnauthenticated, "cache_backend_unauthenticated", faults.NoRetry()
	case http.StatusForbidden:
		code, reason, retry = faults.CodePermissionDenied, "cache_backend_permission_denied", faults.NoRetry()
	case http.StatusNotFound:
		code, reason, retry = faults.CodeNotFound, "cache_object_not_found", faults.NoRetry()
	case http.StatusBadRequest:
		code, reason, retry = faults.CodeInvalidArgument, "cache_backend_rejected_request", faults.NoRetry()
	case http.StatusConflict:
		code, reason, retry = faults.CodeConflict, "cache_backend_conflict", faults.NoRetry()
	case http.StatusPreconditionFailed:
		code, reason, retry = faults.CodeFailedPrecondition, "cache_backend_precondition_failed", faults.NoRetry()
	}
	return faults.New(code, "cache backend request failed", faults.WithReason(reason), faults.WithOperation(operation), faults.WithField("http_status", status), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(retry))
}

func transportFault(operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return faults.Wrap(err, faults.CodeCanceled, "cache backend request canceled", faults.WithReason("cache_backend_canceled"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return faults.Wrap(err, faults.CodeDeadlineExceeded, "cache backend request timed out", faults.WithReason("cache_backend_timeout"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.BackoffRetry(maximumAttempts)))
	}
	return faults.Wrap(err, faults.CodeUnavailable, "cache backend unavailable", faults.WithReason("cache_backend_transport_error"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.BackoffRetry(maximumAttempts)))
}

func invalidConfiguration(operation string) error {
	return faults.New(faults.CodeInvalidArgument, "invalid cache backend configuration", faults.WithReason("invalid_cache_backend_configuration"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
}

func invalidRequest(operation, reason string) error {
	return faults.New(faults.CodeInvalidArgument, "invalid cache backend request", faults.WithReason(reason), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
}

func dataLoss(operation, reason string, err error) error {
	if err == nil {
		return faults.New(faults.CodeDataLoss, "cache object integrity check failed", faults.WithReason(reason), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return faults.Wrap(err, faults.CodeDataLoss, "cache object integrity check failed", faults.WithReason(reason), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
}

func drainAndClose(response *http.Response) {
	if response == nil || response.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumErrorBodyBytes))
	_ = response.Body.Close()
}

func cloneURL(value *url.URL) *url.URL {
	clone := *value
	return &clone
}

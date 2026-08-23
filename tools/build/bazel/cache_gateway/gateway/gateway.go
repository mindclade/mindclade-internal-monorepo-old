// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/blob"
)

type Mode string

const (
	ModeRead  Mode = "read"
	ModeWrite Mode = "write"
)

const (
	protocolVersion = "bazel-http-cache-v1"
	contentType     = "application/octet-stream"

	// DefaultMaximumConcurrentStaging bounds the production launcher's combined GET and PUT spools.
	DefaultMaximumConcurrentStaging = 2
	// MaximumConcurrentStaging prevents a configuration typo from removing the process-level bound.
	MaximumConcurrentStaging = 4
)

type Config struct {
	Mode                     Mode
	InstanceName             string
	MaximumBodyBytes         int64
	MaximumConcurrentStaging int
	TemporaryDir             string
}

type counters struct {
	getHit              atomic.Uint64
	getMiss             atomic.Uint64
	headHit             atomic.Uint64
	headMiss            atomic.Uint64
	putCreated          atomic.Uint64
	putIdempotent       atomic.Uint64
	putRejected         atomic.Uint64
	immutableCollision  atomic.Uint64
	requestError        atomic.Uint64
	readBytes           atomic.Uint64
	writtenBytes        atomic.Uint64
	stagingActive       atomic.Int64
	stagingPeak         atomic.Int64
	stagingWait         atomic.Uint64
	stagingWaitCanceled atomic.Uint64
}

// Store is the cache gateway's deliberately narrow object-store contract. The
// gateway never lists, updates, or deletes cache objects.
type Store interface {
	Put(context.Context, blob.Key, io.Reader, blob.PutOptions) (blob.Attributes, error)
	Open(context.Context, blob.Key, blob.GetOptions) (blob.Object, error)
	Stat(context.Context, blob.Key) (blob.Attributes, error)
}

type Gateway struct {
	store          Store
	mode           Mode
	instancePrefix string
	maximumBytes   int64
	maximumStaging int
	temporaryDir   string
	staging        chan struct{}
	logger         *slog.Logger
	metrics        counters
}

func New(store Store, config Config, logger *slog.Logger) (*Gateway, error) {
	if store == nil {
		return nil, errors.New("cache gateway: blob store is required")
	}
	if config.Mode != ModeRead && config.Mode != ModeWrite {
		return nil, errors.New("cache gateway: mode must be read or write")
	}
	if config.MaximumBodyBytes <= 0 {
		return nil, errors.New("cache gateway: maximum body bytes must be positive")
	}
	maximumStaging := config.MaximumConcurrentStaging
	if maximumStaging == 0 {
		maximumStaging = DefaultMaximumConcurrentStaging
	}
	if maximumStaging < 1 || maximumStaging > MaximumConcurrentStaging {
		return nil, fmt.Errorf("cache gateway: maximum concurrent staging must be between 1 and %d", MaximumConcurrentStaging)
	}
	instancePrefix, err := normalizeInstance(config.InstanceName)
	if err != nil {
		return nil, err
	}
	temporaryDir, err := filepath.Abs(config.TemporaryDir)
	if err != nil {
		return nil, fmt.Errorf("cache gateway: resolve temporary directory: %w", err)
	}
	metadata, err := os.Stat(temporaryDir)
	if err != nil {
		return nil, fmt.Errorf("cache gateway: inspect temporary directory: %w", err)
	}
	if !metadata.IsDir() {
		return nil, errors.New("cache gateway: temporary path must be a directory")
	}
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return &Gateway{
		store:          store,
		mode:           config.Mode,
		instancePrefix: instancePrefix,
		maximumBytes:   config.MaximumBodyBytes,
		maximumStaging: maximumStaging,
		temporaryDir:   temporaryDir,
		staging:        make(chan struct{}, maximumStaging),
		logger:         logger,
	}, nil
}

func (gateway *Gateway) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	setResponseHeaders(response)
	if request.URL.RawPath != "" || request.URL.RawQuery != "" {
		gateway.reject(response, request, http.StatusBadRequest, "non_canonical_request")
		return
	}
	switch request.URL.Path {
	case "/healthz", "/readyz":
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			gateway.rejectMethod(response, request, "GET, HEAD")
			return
		}
		response.WriteHeader(http.StatusOK)
		return
	case "/metrics":
		if request.Method != http.MethodGet {
			gateway.rejectMethod(response, request, http.MethodGet)
			return
		}
		gateway.writeMetrics(response)
		return
	}

	key, kind, digest, err := gateway.cacheKey(request.URL.Path)
	if err != nil {
		gateway.reject(response, request, http.StatusBadRequest, "invalid_cache_key")
		return
	}
	switch request.Method {
	case http.MethodGet:
		gateway.get(response, request, key)
	case http.MethodHead:
		gateway.head(response, request, key)
	case http.MethodPut:
		gateway.put(response, request, key, kind, digest)
	default:
		gateway.rejectMethod(response, request, "GET, HEAD, PUT")
	}
}

func normalizeInstance(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.TrimSpace(value) != value || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") || strings.Contains(value, "\\") {
		return "", errors.New("cache gateway: instance name is not canonical")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("cache gateway: instance name is not canonical")
		}
	}
	return "/" + value, nil
}

func (gateway *Gateway) cacheKey(path string) (blob.Key, string, string, error) {
	prefix := gateway.instancePrefix
	if !strings.HasPrefix(path, prefix+"/") {
		return "", "", "", errors.New("cache gateway: request is outside the configured instance")
	}
	remainder := strings.TrimPrefix(path, prefix+"/")
	parts := strings.Split(remainder, "/")
	if len(parts) != 2 || parts[0] != "ac" && parts[0] != "cas" || !validDigest(parts[1]) {
		return "", "", "", errors.New("cache gateway: invalid cache path")
	}
	key, err := blob.ParseKey(parts[0] + "/" + parts[1])
	if err != nil {
		return "", "", "", err
	}
	return key, parts[0], parts[1], nil
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func (gateway *Gateway) get(response http.ResponseWriter, request *http.Request, key blob.Key) {
	if request.Header.Get("Range") != "" {
		gateway.reject(response, request, http.StatusRequestedRangeNotSatisfiable, "range_not_supported")
		return
	}
	if err := gateway.acquireStaging(request.Context()); err != nil {
		gateway.reject(response, request, http.StatusRequestTimeout, "staging_wait_canceled")
		return
	}
	defer gateway.releaseStaging()
	object, err := gateway.store.Open(request.Context(), key, blob.GetOptions{})
	if err != nil {
		if faults.CodeOf(err) == faults.CodeNotFound {
			gateway.metrics.getMiss.Add(1)
			gateway.reject(response, request, http.StatusNotFound, "cache_miss")
			return
		}
		gateway.backendError(response, request, err)
		return
	}
	staged, size, stageErr := gateway.stageDownload(request.Context(), key, object)
	closeErr := object.Close()
	if stageErr != nil {
		gateway.backendError(response, request, stageErr)
		return
	}
	defer removeStagedFile(staged)
	if closeErr != nil {
		gateway.backendError(response, request, closeErr)
		return
	}
	setObjectHeaders(response, object.Attributes)
	response.WriteHeader(http.StatusOK)
	written, copyErr := io.Copy(response, staged)
	if copyErr != nil || written != size {
		gateway.metrics.requestError.Add(1)
		gateway.logger.Error("cache response stream failed", "code", "response_stream_failed", "kind", cacheKind(key))
		return
	}
	gateway.metrics.getHit.Add(1)
	gateway.metrics.readBytes.Add(uint64(written))
}

func (gateway *Gateway) head(response http.ResponseWriter, request *http.Request, key blob.Key) {
	attributes, err := gateway.store.Stat(request.Context(), key)
	if err != nil {
		if faults.CodeOf(err) == faults.CodeNotFound {
			gateway.metrics.headMiss.Add(1)
			gateway.reject(response, request, http.StatusNotFound, "cache_miss")
			return
		}
		gateway.backendError(response, request, err)
		return
	}
	setObjectHeaders(response, attributes)
	gateway.metrics.headHit.Add(1)
	response.WriteHeader(http.StatusOK)
}

func (gateway *Gateway) put(response http.ResponseWriter, request *http.Request, key blob.Key, kind, expectedDigest string) {
	if gateway.mode != ModeWrite {
		gateway.metrics.putRejected.Add(1)
		response.Header().Set("Allow", "GET, HEAD")
		gateway.reject(response, request, http.StatusMethodNotAllowed, "read_only")
		return
	}
	if request.ContentLength > gateway.maximumBytes {
		gateway.reject(response, request, http.StatusRequestEntityTooLarge, "object_too_large")
		return
	}
	if err := gateway.acquireStaging(request.Context()); err != nil {
		gateway.reject(response, request, http.StatusRequestTimeout, "staging_wait_canceled")
		return
	}
	defer gateway.releaseStaging()

	staged, size, digest, err := gateway.stage(request.Context(), request.Body)
	if err != nil {
		var status int
		var code string
		if errors.Is(err, errObjectTooLarge) {
			status, code = http.StatusRequestEntityTooLarge, "object_too_large"
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status, code = http.StatusRequestTimeout, "request_canceled"
		} else {
			status, code = http.StatusBadRequest, "request_body_failed"
		}
		gateway.reject(response, request, status, code)
		return
	}
	defer removeStagedFile(staged)
	if request.ContentLength >= 0 && request.ContentLength != size {
		gateway.reject(response, request, http.StatusBadRequest, "content_length_mismatch")
		return
	}
	if kind == "cas" && digest.Hex() != expectedDigest {
		gateway.reject(response, request, http.StatusBadRequest, "cas_digest_mismatch")
		return
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		gateway.backendError(response, request, err)
		return
	}
	attributes, err := gateway.store.Put(request.Context(), key, staged, blob.PutOptions{
		ContentType: contentType,
		Metadata: blob.Metadata{
			"cache-kind":       kind,
			"protocol-version": protocolVersion,
		},
		Digest: digest,
		Preconditions: blob.Preconditions{
			IfNotExists: true,
		},
	})
	if err == nil {
		gateway.metrics.putCreated.Add(1)
		gateway.metrics.writtenBytes.Add(uint64(size))
		setObjectHeaders(response, attributes)
		response.WriteHeader(http.StatusOK)
		return
	}
	if faults.CodeOf(err) != faults.CodeAlreadyExists {
		gateway.backendError(response, request, err)
		return
	}
	existing, statErr := gateway.store.Stat(request.Context(), key)
	if statErr != nil {
		gateway.backendError(response, request, statErr)
		return
	}
	if existing.Size == size && existing.Digest.Equal(digest) {
		gateway.metrics.putIdempotent.Add(1)
		setObjectHeaders(response, existing)
		response.WriteHeader(http.StatusOK)
		return
	}
	gateway.metrics.immutableCollision.Add(1)
	gateway.reject(response, request, http.StatusConflict, "immutable_collision")
}

var errObjectTooLarge = errors.New("cache gateway: object too large")

func (gateway *Gateway) acquireStaging(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case gateway.staging <- struct{}{}:
		gateway.recordStagingAcquired()
		return nil
	default:
		gateway.metrics.stagingWait.Add(1)
	}
	select {
	case gateway.staging <- struct{}{}:
		gateway.recordStagingAcquired()
		return nil
	case <-ctx.Done():
		gateway.metrics.stagingWaitCanceled.Add(1)
		return ctx.Err()
	}
}

func (gateway *Gateway) recordStagingAcquired() {
	active := gateway.metrics.stagingActive.Add(1)
	for {
		peak := gateway.metrics.stagingPeak.Load()
		if active <= peak || gateway.metrics.stagingPeak.CompareAndSwap(peak, active) {
			return
		}
	}
}

func (gateway *Gateway) releaseStaging() {
	gateway.metrics.stagingActive.Add(-1)
	<-gateway.staging
}

func (gateway *Gateway) stageDownload(ctx context.Context, key blob.Key, object blob.Object) (*os.File, int64, error) {
	if object.Body == nil || object.Attributes.Key != key || object.Attributes.Validate() != nil || !object.Attributes.Digest.Valid() || object.Attributes.Size > gateway.maximumBytes {
		return nil, 0, downloadIntegrityError()
	}
	file, err := os.CreateTemp(gateway.temporaryDir, ".bazel-cache-download-*")
	if err != nil {
		return nil, 0, err
	}
	cleanup := func(cause error) (*os.File, int64, error) {
		removeStagedFile(file)
		return nil, 0, cause
	}
	if err := file.Chmod(0o600); err != nil {
		return cleanup(err)
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(object.Body, gateway.maximumBytes+1))
	if err != nil {
		return cleanup(err)
	}
	if err := ctx.Err(); err != nil {
		return cleanup(err)
	}
	digest, err := identifiers.DigestFromBytes(hash.Sum(nil))
	if err != nil {
		return cleanup(downloadIntegrityError())
	}
	if size != object.Attributes.Size || size > gateway.maximumBytes || !digest.Equal(object.Attributes.Digest) {
		return cleanup(downloadIntegrityError())
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return cleanup(err)
	}
	return file, size, nil
}

func downloadIntegrityError() error {
	return faults.New(
		faults.CodeDataLoss,
		"cache object integrity check failed",
		faults.WithReason("cache_download_integrity_mismatch"),
		faults.WithOperation("cache_gateway.stageDownload"),
		faults.WithRetryPolicy(faults.NoRetry()),
	)
}

func removeStagedFile(file *os.File) {
	if file == nil {
		return
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
}

func (gateway *Gateway) stage(ctx context.Context, source io.Reader) (*os.File, int64, identifiers.Digest, error) {
	file, err := os.CreateTemp(gateway.temporaryDir, ".bazel-cache-upload-*")
	if err != nil {
		return nil, 0, identifiers.Digest{}, err
	}
	cleanup := func(cause error) (*os.File, int64, identifiers.Digest, error) {
		name := file.Name()
		_ = file.Close()
		_ = os.Remove(name)
		return nil, 0, identifiers.Digest{}, cause
	}
	if err := file.Chmod(0o600); err != nil {
		return cleanup(err)
	}
	hash := sha256.New()
	limited := io.LimitReader(source, gateway.maximumBytes+1)
	size, err := io.Copy(file, io.TeeReader(limited, hash))
	if err != nil {
		return cleanup(err)
	}
	if err := ctx.Err(); err != nil {
		return cleanup(err)
	}
	if size > gateway.maximumBytes {
		return cleanup(errObjectTooLarge)
	}
	digest, err := identifiers.DigestFromBytes(hash.Sum(nil))
	if err != nil {
		return cleanup(err)
	}
	return file, size, digest, nil
}

func (gateway *Gateway) writeMetrics(response http.ResponseWriter) {
	response.Header().Set("Content-Type", "application/json")
	payload := map[string]any{
		"schema_version":             2,
		"protocol":                   protocolVersion,
		"mode":                       gateway.mode,
		"get_hit":                    gateway.metrics.getHit.Load(),
		"get_miss":                   gateway.metrics.getMiss.Load(),
		"head_hit":                   gateway.metrics.headHit.Load(),
		"head_miss":                  gateway.metrics.headMiss.Load(),
		"put_created":                gateway.metrics.putCreated.Load(),
		"put_idempotent":             gateway.metrics.putIdempotent.Load(),
		"put_rejected":               gateway.metrics.putRejected.Load(),
		"immutable_collision":        gateway.metrics.immutableCollision.Load(),
		"request_error":              gateway.metrics.requestError.Load(),
		"read_bytes":                 gateway.metrics.readBytes.Load(),
		"written_bytes":              gateway.metrics.writtenBytes.Load(),
		"maximum_concurrent_staging": gateway.maximumStaging,
		"staging_active":             gateway.metrics.stagingActive.Load(),
		"staging_peak":               gateway.metrics.stagingPeak.Load(),
		"staging_wait":               gateway.metrics.stagingWait.Load(),
		"staging_wait_canceled":      gateway.metrics.stagingWaitCanceled.Load(),
	}
	if err := json.NewEncoder(response).Encode(payload); err != nil {
		gateway.metrics.requestError.Add(1)
		gateway.logger.Error("cache metrics response failed", "code", "metrics_response_failed")
	}
}

func (gateway *Gateway) backendError(response http.ResponseWriter, request *http.Request, err error) {
	code := faults.CodeOf(err)
	status := http.StatusBadGateway
	if code == faults.CodePermissionDenied || code == faults.CodeUnauthenticated {
		status = http.StatusServiceUnavailable
	}
	gateway.logger.Error("cache backend operation failed", "code", "backend_error", "fault_code", code, "method", request.Method)
	gateway.reject(response, request, status, "backend_error")
}

func (gateway *Gateway) rejectMethod(response http.ResponseWriter, request *http.Request, allow string) {
	response.Header().Set("Allow", allow)
	gateway.reject(response, request, http.StatusMethodNotAllowed, "method_not_allowed")
}

func (gateway *Gateway) reject(response http.ResponseWriter, request *http.Request, status int, code string) {
	response.Header().Set("X-Mindclade-Error-Code", code)
	http.Error(response, http.StatusText(status), status)
	if status >= http.StatusInternalServerError {
		gateway.metrics.requestError.Add(1)
	}
	if status != http.StatusNotFound {
		gateway.logger.Warn("cache request rejected", "code", code, "method", request.Method, "status", status)
	}
}

func setResponseHeaders(response http.ResponseWriter) {
	response.Header().Set("Cache-Control", "private, no-transform")
	response.Header().Set("X-Content-Type-Options", "nosniff")
}

func setObjectHeaders(response http.ResponseWriter, attributes blob.Attributes) {
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("Content-Length", strconv.FormatInt(attributes.Size, 10))
	if attributes.ETag != "" {
		response.Header().Set("ETag", attributes.ETag)
	}
	response.Header().Set("X-Mindclade-Object-Generation", strconv.FormatInt(attributes.Generation, 10))
}

func cacheKind(key blob.Key) string {
	kind, _, _ := strings.Cut(key.String(), "/")
	return kind
}

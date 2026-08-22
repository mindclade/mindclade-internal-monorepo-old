// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package gcshttp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/storage/blob"
)

const (
	testBucket = "mc-common-ci-bazel-cache"
	testPrefix = "bazel-http-cache/v1"
)

func TestPutIsCreateOnlyAndServerValidated(t *testing.T) {
	t.Parallel()
	payload := []byte("cache payload")
	digest := identifiers.SHA256(payload)
	key := blob.MustParseKey("cas/" + digest.Hex())
	var putCount int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPut:
			putCount++
			if got, want := request.URL.Path, "/"+testBucket+"/"+testPrefix+"/"+key.String(); got != want {
				t.Errorf("PUT path = %q, want %q", got, want)
			}
			if got := request.Header.Get("X-Goog-If-Generation-Match"); got != "0" {
				t.Errorf("generation precondition = %q", got)
			}
			if got := request.Header.Get("X-Goog-Meta-Mindclade-Sha256"); got != digest.String() {
				t.Errorf("digest metadata = %q, want %q", got, digest.String())
			}
			if got := request.Header.Get("X-Goog-Hash"); got != "crc32c="+crc32c(payload) {
				t.Errorf("integrity header = %q", got)
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Errorf("read upload: %v", err)
			}
			if !bytes.Equal(body, payload) {
				t.Errorf("upload = %q, want %q", body, payload)
			}
			response.WriteHeader(http.StatusOK)
		case http.MethodHead:
			writeObjectHeaders(response.Header(), digest, int64(len(payload)), 41)
			response.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected method %s", request.Method)
			response.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)

	store := testStore(t, server.URL, int64(len(payload)))
	attributes, err := store.Put(context.Background(), key, bytes.NewReader(payload), blob.PutOptions{
		ContentType: "application/octet-stream",
		Metadata: blob.Metadata{
			"cache-kind":       "cas",
			"protocol-version": "bazel-http-cache-v1",
		},
		Digest: digest,
		Preconditions: blob.Preconditions{
			IfNotExists: true,
		},
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if putCount != 1 {
		t.Fatalf("PUT requests = %d, want 1", putCount)
	}
	if attributes.Generation != 41 || attributes.Size != int64(len(payload)) || !attributes.Digest.Equal(digest) {
		t.Fatalf("Put() attributes = %+v", attributes)
	}
}

func TestPutMapsGenerationPreconditionToAlreadyExists(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusPreconditionFailed)
	}))
	t.Cleanup(server.Close)
	payload := []byte("duplicate")
	digest := identifiers.SHA256(payload)
	store := testStore(t, server.URL, int64(len(payload)))
	_, err := store.Put(context.Background(), blob.MustParseKey("ac/"+digest.Hex()), bytes.NewReader(payload), blob.PutOptions{
		ContentType: "application/octet-stream",
		Digest:      digest,
		Preconditions: blob.Preconditions{
			IfNotExists: true,
		},
	})
	if got := faults.CodeOf(err); got != faults.CodeAlreadyExists {
		t.Fatalf("Put() code = %q, want %q; error = %v", got, faults.CodeAlreadyExists, err)
	}
}

func TestPutRetriesTransientFailureWithSameCreateOnlyBody(t *testing.T) {
	t.Parallel()
	payload := []byte("retry payload")
	digest := identifiers.SHA256(payload)
	key := blob.MustParseKey("cas/" + digest.Hex())
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPut:
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Errorf("read retry upload: %v", err)
			}
			if !bytes.Equal(body, payload) || request.Header.Get("X-Goog-If-Generation-Match") != "0" {
				t.Errorf("retry upload lost body or generation-zero precondition")
			}
			if attempts.Add(1) == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			response.WriteHeader(http.StatusOK)
		case http.MethodHead:
			writeObjectHeaders(response.Header(), digest, int64(len(payload)), 51)
			response.WriteHeader(http.StatusOK)
		default:
			response.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	store := testStore(t, server.URL, 1024)
	_, err := store.Put(context.Background(), key, bytes.NewReader(payload), blob.PutOptions{
		ContentType: "application/octet-stream",
		Digest:      digest,
		Preconditions: blob.Preconditions{
			IfNotExists: true,
		},
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("PUT attempts = %d, want 2", got)
	}
}

func TestOpenPinsGenerationAndVerifiesDigest(t *testing.T) {
	t.Parallel()
	payload := []byte("verified")
	digest := identifiers.SHA256(payload)
	key := blob.MustParseKey("cas/" + digest.Hex())
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		writeObjectHeaders(response.Header(), digest, int64(len(payload)), 73)
		switch request.Method {
		case http.MethodHead:
			if request.URL.Query().Get("generation") != "" {
				t.Errorf("initial HEAD unexpectedly pinned: %s", request.URL.RawQuery)
			}
			response.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if got := request.URL.Query().Get("generation"); got != "73" {
				t.Errorf("GET generation = %q, want 73", got)
			}
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write(payload)
		default:
			response.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	store := testStore(t, server.URL, 1024)
	object, err := store.Open(context.Background(), key, blob.GetOptions{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer object.Close()
	got, err := io.ReadAll(object.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("download = %q, want %q", got, payload)
	}
}

func TestOpenRejectsSameSizeCorruption(t *testing.T) {
	t.Parallel()
	expected := []byte("expected")
	corrupt := []byte("corrupt!")
	digest := identifiers.SHA256(expected)
	key := blob.MustParseKey("cas/" + digest.Hex())
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		writeObjectHeaders(response.Header(), digest, int64(len(expected)), 91)
		response.WriteHeader(http.StatusOK)
		if request.Method == http.MethodGet {
			_, _ = response.Write(corrupt)
		}
	}))
	t.Cleanup(server.Close)
	store := testStore(t, server.URL, 1024)
	object, err := store.Open(context.Background(), key, blob.GetOptions{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer object.Close()
	_, err = io.ReadAll(object.Body)
	if got := faults.CodeOf(err); got != faults.CodeDataLoss {
		t.Fatalf("ReadAll() code = %q, want %q; error = %v", got, faults.CodeDataLoss, err)
	}
}

func TestBackendErrorBodyIsNotReflected(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusForbidden)
		_, _ = response.Write([]byte("credential-shaped-sensitive-error"))
	}))
	t.Cleanup(server.Close)
	store := testStore(t, server.URL, 1024)
	_, err := store.Stat(context.Background(), blob.MustParseKey("ac/"+strings.Repeat("0", 64)))
	if got := faults.CodeOf(err); got != faults.CodePermissionDenied {
		t.Fatalf("Stat() code = %q, want %q", got, faults.CodePermissionDenied)
	}
	if strings.Contains(err.Error(), "credential-shaped-sensitive-error") {
		t.Fatal("backend response body leaked into error")
	}
}

func TestStoreRejectsNonCreateOnlyPut(t *testing.T) {
	t.Parallel()
	store := testStore(t, "http://127.0.0.1:1", 1024)
	payload := []byte("payload")
	_, err := store.Put(context.Background(), blob.MustParseKey("ac/"+strings.Repeat("0", 64)), bytes.NewReader(payload), blob.PutOptions{
		ContentType: "application/octet-stream",
		Digest:      identifiers.SHA256(payload),
	})
	if got := faults.CodeOf(err); got != faults.CodeInvalidArgument {
		t.Fatalf("Put() code = %q, want %q", got, faults.CodeInvalidArgument)
	}
}

func TestStoreRejectsMismatchedUploadDigestBeforeNetwork(t *testing.T) {
	t.Parallel()
	store := testStore(t, "http://127.0.0.1:1", 1024)
	payload := []byte("payload")
	_, err := store.Put(context.Background(), blob.MustParseKey("ac/"+strings.Repeat("0", 64)), bytes.NewReader(payload), blob.PutOptions{
		ContentType: "application/octet-stream",
		Digest:      identifiers.SHA256([]byte("different")),
		Preconditions: blob.Preconditions{
			IfNotExists: true,
		},
	})
	if got := faults.CodeOf(err); got != faults.CodeDataLoss {
		t.Fatalf("Put() code = %q, want %q", got, faults.CodeDataLoss)
	}
}

func testStore(t *testing.T, endpoint string, maximumBytes int64) *Store {
	t.Helper()
	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	store, err := newStore(http.DefaultClient, testBucket, testPrefix, maximumBytes, parsed)
	if err != nil {
		t.Fatalf("newStore() error = %v", err)
	}
	return store
}

func writeObjectHeaders(headers http.Header, digest identifiers.Digest, size, generation int64) {
	headers.Set("Content-Length", strconv.FormatInt(size, 10))
	headers.Set("Content-Type", "application/octet-stream")
	headers.Set("ETag", "immutable-etag")
	headers.Set("Last-Modified", "Fri, 22 Aug 2026 12:00:00 GMT")
	headers.Set("X-Goog-Generation", strconv.FormatInt(generation, 10))
	headers.Set("X-Goog-Meta-Mindclade-Sha256", digest.String())
	headers.Set("X-Goog-Meta-Cache-Kind", "cas")
	headers.Set("X-Goog-Meta-Protocol-Version", "bazel-http-cache-v1")
}

func crc32c(payload []byte) string {
	checksum := crc32.Checksum(payload, crc32.MakeTable(crc32.Castagnoli))
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], checksum)
	return base64.StdEncoding.EncodeToString(encoded[:])
}

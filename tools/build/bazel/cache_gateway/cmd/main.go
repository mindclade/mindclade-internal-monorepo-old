// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/storage/blob"
	"go.mindclade.dev/tools/build/bazel/cache_gateway/gateway"
	"go.mindclade.dev/tools/build/bazel/cache_gateway/gcshttp"
)

const defaultMaximumBodyBytes int64 = 1 << 30

const (
	storageReadOnlyScope  = "https://www.googleapis.com/auth/devstorage.read_only"
	storageReadWriteScope = "https://www.googleapis.com/auth/devstorage.read_write"
)

var _ gateway.Store = (*gcshttp.Store)(nil)

func main() {
	listenAddress := flag.String("listen-address", "127.0.0.1:8085", "loopback listen address")
	bucket := flag.String("bucket", "", "exact Cloud Storage bucket")
	prefix := flag.String("prefix", "bazel-http-cache/v1", "object prefix")
	instanceName := flag.String("instance-name", "", "exact Bazel HTTP cache instance")
	mode := flag.String("mode", "", "cache access mode: read or write")
	maximumBodyBytes := flag.Int64("maximum-body-bytes", defaultMaximumBodyBytes, "maximum accepted cache object")
	maximumConcurrentStaging := flag.Int("maximum-concurrent-staging", gateway.DefaultMaximumConcurrentStaging, "maximum concurrent GET and PUT staging files")
	temporaryDirectory := flag.String("temporary-directory", "", "GET and PUT staging directory")
	readyFile := flag.String("ready-file", "", "readiness sentinel written after backend access succeeds")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if flag.NArg() != 0 || *bucket == "" || *temporaryDirectory == "" || *readyFile == "" {
		fatal(logger, "invalid_configuration", errors.New("bucket, temporary-directory, ready-file, and zero positional arguments are required"))
	}
	if *listenAddress == "" || !isLoopbackAddress(*listenAddress) {
		fatal(logger, "invalid_listen_address", errors.New("listen-address must resolve to loopback"))
	}
	if *maximumBodyBytes <= 0 || *maximumBodyBytes > defaultMaximumBodyBytes {
		fatal(logger, "invalid_maximum_body", errors.New("maximum-body-bytes must be between one byte and one GiB"))
	}
	if *maximumConcurrentStaging < 1 || *maximumConcurrentStaging > gateway.MaximumConcurrentStaging {
		fatal(logger, "invalid_maximum_staging", fmt.Errorf("maximum-concurrent-staging must be between 1 and %d", gateway.MaximumConcurrentStaging))
	}
	cacheMode := gateway.Mode(*mode)
	scope := storageReadOnlyScope
	if cacheMode == gateway.ModeWrite {
		scope = storageReadWriteScope
	} else if cacheMode != gateway.ModeRead {
		fatal(logger, "invalid_mode", errors.New("mode must be read or write"))
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	credentials, err := google.FindDefaultCredentials(ctx, scope)
	if err != nil {
		fatal(logger, "gcs_credentials_failed", err)
	}
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		fatal(logger, "http_transport_failed", errors.New("default HTTP transport has unexpected type"))
	}
	transport := defaultTransport.Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.IdleConnTimeout = 90 * time.Second
	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: oauth2.ReuseTokenSource(nil, credentials.TokenSource),
			Base:   transport,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	store, err := gcshttp.New(client, *bucket, *prefix, *maximumBodyBytes)
	if err != nil {
		fatal(logger, "gcs_store_failed", err)
	}
	if err := proveReadAccess(ctx, store); err != nil {
		fatal(logger, "gcs_read_probe_failed", err)
	}
	handler, err := gateway.New(store, gateway.Config{
		Mode:                     cacheMode,
		InstanceName:             *instanceName,
		MaximumBodyBytes:         *maximumBodyBytes,
		MaximumConcurrentStaging: *maximumConcurrentStaging,
		TemporaryDir:             *temporaryDirectory,
	}, logger)
	if err != nil {
		fatal(logger, "gateway_configuration_failed", err)
	}
	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		fatal(logger, "listen_failed", err)
	}
	if err := writeReadyFile(*readyFile, listener.Addr().String(), *mode); err != nil {
		_ = listener.Close()
		fatal(logger, "ready_file_failed", err)
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("cache gateway shutdown failed", "code", "shutdown_failed")
		}
	}()

	logger.Info("cache gateway ready", "mode", *mode, "protocol", "bazel-http-cache-v1")
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal(logger, "serve_failed", err)
	}
}

func proveReadAccess(ctx context.Context, store gateway.Store) error {
	_, err := store.Stat(ctx, blob.MustParseKey("_gateway/readiness-probe"))
	if err == nil || faults.CodeOf(err) == faults.CodeNotFound {
		return nil
	}
	return errors.New("cache backend read probe failed")
}

func isLoopbackAddress(value string) bool {
	host, port, err := net.SplitHostPort(value)
	if err != nil || port == "" {
		return false
	}
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		return false
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func writeReadyFile(path, address, mode string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(file, "schema_version=1\naddress=%s\nmode=%s\n", address, mode); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func fatal(logger *slog.Logger, code string, err error) {
	logger.Error("cache gateway failed", "code", code, "error_type", fmt.Sprintf("%T", err))
	os.Exit(1)
}

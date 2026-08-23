// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Command go_vanity serves the go-import meta tags for go.mindclade.dev.
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go.mindclade.dev/services/go_vanity/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	config := configFromEnvironment()
	runtime, err := service.New(config.service)
	if err != nil {
		logger.Error("invalid vanity configuration", "error", err)
		os.Exit(1)
	}

	appListener, err := net.Listen("tcp", config.listenAddress)
	if err != nil {
		logger.Error("application listener failed", "address", config.listenAddress, "error", err)
		os.Exit(1)
	}
	metricsListener, err := net.Listen("tcp", config.metricsAddress)
	if err != nil {
		_ = appListener.Close()
		logger.Error("metrics listener failed", "address", config.metricsAddress, "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info(
		"serving go-import meta tags",
		"address", config.listenAddress,
		"metrics_address", config.metricsAddress,
		"module_path", config.service.ModulePath,
		"repo_url", config.service.RepoURL,
	)

	if err := runtime.Serve(ctx, appListener, metricsListener); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

type processConfig struct {
	listenAddress  string
	metricsAddress string
	service        service.Config
}

func configFromEnvironment() processConfig {
	return processConfig{
		listenAddress:  envOr("LISTEN_ADDRESS", ":8080"),
		metricsAddress: envOr("METRICS_LISTEN_ADDRESS", "127.0.0.1:9090"),
		service: service.Config{
			ModulePath: envOr("MODULE_PATH", "go.mindclade.dev"),
			RepoURL:    envOr("MODULE_REPO_URL", "https://github.com/mindclade/mindclade-internal-monorepo"),
			DocsURL:    envOr("DOCS_URL", "https://docs.mindclade.dev"),
		},
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

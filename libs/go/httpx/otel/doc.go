// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package otel provides optional OpenTelemetry adapters for ordinary HTTP
// handlers and transports. Do not wrap a Connect RPC route with both otelhttp
// and otelconnect unless duplicate spans are intentionally desired.
package otel

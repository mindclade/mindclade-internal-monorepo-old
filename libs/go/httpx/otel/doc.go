// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package otel provides optional OpenTelemetry adapters for ordinary HTTP
// handlers and transports. Do not wrap a Connect RPC route with both otelhttp
// and otelconnect unless duplicate spans are intentionally desired.
package otel

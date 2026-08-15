// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package observability provides provider-neutral conventions and runtime glue
// for Mindclade logging, metrics, telemetry propagation, and telemetry
// lifecycle management.
//
// The package does not install process-wide globals and does not implement a
// tracing or metrics backend. Concrete OpenTelemetry providers, exporters, and
// transport instrumentation are injected by service composition code. This
// package supplies safe slog enrichment and redaction, low-cardinality metric
// records, request and trace propagation coordination, resource metadata, and
// deterministic test adapters.
package observability

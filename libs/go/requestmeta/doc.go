// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package requestmeta defines transport-neutral request identity and
// correlation metadata for Mindclade services.
//
// The package is authoritative for request IDs, correlation IDs, causation
// IDs, and logical operation names. HTTP, Connect, gRPC, queue, and workflow
// adapters should extract transport values into Metadata and place that value
// in context. Authentication principals and OpenTelemetry span context remain
// owned by their respective packages.
package requestmeta

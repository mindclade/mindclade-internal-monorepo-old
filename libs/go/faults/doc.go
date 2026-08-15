// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package faults provides transport-neutral structured errors for Mindclade Go
// services and libraries.
//
// A fault has a stable code, a client-safe message, optional machine-readable
// reason and operation, structured diagnostic fields, explicit retry intent,
// and an optional wrapped cause. The package deliberately does not depend on
// HTTP, gRPC, Connect, logging, telemetry, persistence, or workflow runtimes.
//
// External transports should serialize CodeOf, PublicMessageOf, ReasonOf, and
// carefully selected FieldsOf values. They must not expose Error directly,
// because Error includes the wrapped cause for diagnostic usefulness.
package faults

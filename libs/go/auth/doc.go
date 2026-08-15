// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package auth defines transport-neutral authentication and authorization
// contracts for Mindclade services.
//
// Provider adapters verify credentials and return immutable Principals.
// Services authorize explicit Permission and Resource requests through an
// Authorizer. HTTP, Connect, and gRPC extraction and response rendering belong
// in their respective transport packages.
package auth

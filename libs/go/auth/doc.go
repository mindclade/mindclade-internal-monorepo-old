// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

// Package auth defines transport-neutral authentication and authorization
// contracts for Mindclade services.
//
// Provider adapters verify credentials and return immutable Principals.
// Services authorize explicit Permission and Resource requests through an
// Authorizer. HTTP, Connect, and gRPC extraction and response rendering belong
// in their respective transport packages.
package auth

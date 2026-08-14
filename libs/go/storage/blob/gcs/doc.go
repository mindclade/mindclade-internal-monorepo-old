// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

// Package gcs implements blob.Store with Google Cloud Storage.
//
// The store preserves Mindclade SHA-256 digests in object metadata and spools
// uploads before committing them so an expected digest can be verified before
// the destination object becomes visible.
package gcs

// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package gcs implements blob.Store with Google Cloud Storage.
//
// The store preserves Mindclade SHA-256 digests in object metadata and spools
// uploads before committing them so an expected digest can be verified before
// the destination object becomes visible.
package gcs

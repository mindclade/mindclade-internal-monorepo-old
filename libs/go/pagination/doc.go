// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package pagination defines the repository-wide opaque keyset-pagination
// contract. It binds signed cursors to scope, resource, normalized filters, and
// ordering so tokens cannot be replayed against a different query.
package pagination

// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package pagination defines the repository-wide opaque keyset-pagination
// contract. It binds signed cursors to scope, resource, normalized filters, and
// ordering so tokens cannot be replayed against a different query.
package pagination

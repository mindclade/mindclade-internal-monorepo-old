// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package provider is the control-plane composition root. It is the only place
// in the service that names concrete providers: it opens the PostgreSQL pool,
// constructs the Google Cloud Storage and Redis clients, and binds them to the
// libs/go contracts carried by foundation.Dependencies.
//
// Provider selection is deliberately not configurable per store. A role either
// needs a durable mechanism or it does not; when it does, this package
// constructs the single supported production adapter and fails closed if the
// deployment has not configured it. In-memory adapters belong to tests and to
// the reference slices under examples/go, never to a production factory.
//
// Domain policy stays out: no repositories, no route tables, no generated
// handlers, and no business services are assembled here.
package providers

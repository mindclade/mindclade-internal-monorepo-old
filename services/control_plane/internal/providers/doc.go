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
// deployment has not configured it.
//
// Messaging is the one exception, and it is bounded rather than open. The
// in-memory broker is selectable, because no Pub/Sub SDK is in go.mod yet and
// a dispatcher that cannot be run at all is not safer than one that can be run
// locally. It is refused outside a development or test environment by two
// independent gates -- config.Settings.Validate and newPublisher -- because a
// process-local broker in production would turn a delivery outage into a
// silent success. No other store may follow this pattern: when the Pub/Sub
// module lands, the memory branch goes back to tests where it belongs.
//
// This package (the shared mechanism package itself) contains no domain policy,
// repositories, or route tables. Role packages beneath internal/providers are
// the Layer-5 process composition roots accepted by ADR-0010 and ADR-0015: they
// may bind reusable control/ domain services to concrete repositories and
// transports. Keeping that binding in a role package makes it testable and
// prevents either the command or reusable domain package from selecting a
// production provider.
package providers

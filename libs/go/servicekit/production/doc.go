// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package production defines the narrow, repository-wide process profiles
// used to prove that a Go service composition includes the production
// mechanisms required for its role.
//
// The package deliberately does not construct databases, transports,
// Kubernetes clients, or domain services. Composition roots discover the
// capabilities they actually wired, validate a Manifest, and then use the
// returned servicekit Assembly. This keeps provider construction and domain
// policy outside libs/go while enforcing one lifecycle and readiness path.
package production

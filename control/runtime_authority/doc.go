// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package runtime_authority owns durable Go control-plane authority for signed
// admission grants, execution tickets, route snapshots, and revocations.
//
// It deliberately does not perform online request routing or node-local
// admission. Go publishes immutable policy snapshots and signed capabilities;
// the Rust runtime validates and consumes them locally.
package runtime_authority

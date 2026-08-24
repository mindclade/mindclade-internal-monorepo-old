// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package schedulingtest is the shared conformance suite every
// scheduling.Repository implementation must pass.
//
// It exists because Repository is the seam that lets the control plane run its
// capacity ledger in process for a test and on PostgreSQL in production. That
// promise is only worth something if the two adapters agree on the awkward
// cases, and the awkward cases are exactly the ones each adapter's own tests
// are least likely to invent independently: a redelivered placement that must
// replay instead of charging the ledger twice, a placement key reused by a
// different workload, a leader that was replaced mid-transition, a decision
// taken against a fleet that has since moved, and a deadline that has to be
// applied as a write before any ledger read can be trusted.
//
// # Reason strings are the contract
//
// Every assertion below names a faults code AND a reason string, because a
// caller switching on faults.IsReason is the thing that breaks when a factory
// swaps adapters. "An error happened" is not conformance; the same error is.
// The one deliberate exception is a cancelled context, which is argued at its
// assertion.
//
// # What the suite does not assert
//
// The two adapters differ in three named, reviewed places, and the suite stays
// out of all three rather than pretending they agree:
//
//   - MemoryRepository enforces a record-count bound (reservation_store_bound)
//     because it is an in-process map nothing evicts. The PostgreSQL store
//     deliberately does not, because refusing to record a reservation would
//     leave the cluster holding capacity the ledger cannot see.
//   - MemoryRepository can raise placement_index_corrupt; the SQL schema makes
//     the placement key a UNIQUE column on the reservation row, so finding the
//     key is finding the row and the case does not exist.
//   - The SQL expiry sweep is bounded (MaximumExpirySweep) while the reference
//     adapter's is not, so the two answer differently only once more holds have
//     lapsed in one instant than the batch covers. Every fixture here expires
//     one or two holds, well inside that batch, so the suite compares the
//     semantics rather than the batch size.
//
// # Fleet configuration
//
// Repository has no method that records a quota or a weight, but a repository
// with neither admits nothing at all -- every ClusterQueue ships with a zero
// nominal quota and stopPolicy Hold. Both adapters expose the same two
// configuration methods beside the interface, so the suite requires them
// through Fleet and says so if an adapter does not have them.
package schedulingtest

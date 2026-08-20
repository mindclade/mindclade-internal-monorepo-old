// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package postgres implements the durable repository contracts that
// control/registry declares, backed by PostgreSQL.
//
// The domain packages own their own seams: control/registry/models declares
// models.Repository and control/registry/releases declares releases.Repository.
// This package implements those interfaces rather than inventing its own, so
// the storage layer cannot drift from the shape the domain services actually
// call. A repository here applies no publication or promotion policy; the
// domain Service has already validated, sealed, and decided by the time a
// value reaches storage.
//
// The adapters are transaction-aware in the same way every libs/go adapter is:
// a method takes context.Context only, never an explicit *sql.Tx, and joins
// whatever transaction storage/sql/transaction has installed on the context.
// That is what lets a domain mutation, its audit record, and its outbox insert
// reach one commit. A repository that opened its own transaction could not
// join that commit, and the mutation-and-publication contract in
// GO_FOUNDATION_CONSUMPTION.md would be unenforceable.
//
// # Concurrency control differs per record, because the domain models differ
//
// A models.Descriptor is content-addressed: SealDigest hashes the canonical
// encoding of every other field, Lifecycle included, so no in-place update is
// representable. Changing a descriptor produces a different DescriptorDigest,
// which is a different row. PutDescriptor is therefore insert-if-absent, and a
// republish of identical content is a silent no-op rather than a conflict.
// Republishing different content under a digest that is already taken is a
// digest collision, not a concurrent write, and is refused.
//
// A releases.Release is mutable state under a stable ReleaseID -- Promote
// moves Status from qualified to promoted -- and carries its own
// ResourceVersion counter. PutRelease compare-and-swaps on it. This package
// does not reach for libs/go/resourceversion here: Release already declares
// its optimistic-concurrency field, and pairing a second mechanism beside it
// would leave two versions to keep agreeing.
//
// An EvidenceGraph is sealed by the digest the Release quotes, so it is
// immutable once written and PutGraph is insert-if-absent on ReleaseID.
//
// # What is not here
//
// control/registry/datasets, checkpoints, and reference_databases are still
// production-blueprint scaffolds that declare no repository seam. No table or
// adapter is written for them, because storage built ahead of the contract it
// serves is storage that gets rewritten.
package postgres

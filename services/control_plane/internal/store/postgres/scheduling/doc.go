// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package schedulingpostgres implements control/scheduling.Repository on
// PostgreSQL.
//
// The domain ships scheduling.MemoryRepository as its reference adapter and
// calls it "the executable specification of the transaction the SQL
// implementation owes". This package is that implementation, written to agree
// with the reference adapter decision for decision. Where the two differ, the
// difference is deliberate and named in a comment.
//
// # What the interface demands and how it is met here
//
// Repository's contract is that every mutation is atomic with its audit record
// and its outbox append, in one transaction, and that the capacity ledger is
// re-checked inside that same transaction as the reservation write. Every
// method below therefore runs through runMutation, which opens exactly one
// SERIALIZABLE transaction, installs it on the context, and lets the audit
// recorder and the outbox store join it through storage/sql/transaction.
//
// # Five places this store is not the orchestration adapter
//
// 1. Snapshot and Held are writes. Both re-seal expired holds before they read
// the ledger, because the domain's rule is that "an expired hold never appears
// as occupied capacity". A read that returns capacity as busy when it is merely
// unaccounted for is the failure lazy expiry exists to prevent, so both run
// inside runMutation. Get is the only pure read in this package -- it names one
// reservation, reads no ledger, and therefore has nothing to expire.
//
// 2. There is no digest to restate. Orchestration had to reimplement an
// unexported seal because its transition was a field assignment. Every
// scheduling transition is an exported method on scheduling.Reservation that
// seals and revalidates internally, so this package calls current.Bind(...) and
// stores what comes back. Nothing here recomputes a version, and there is no
// second definition that could drift.
//
// 3. Reserve recomputes the fleet fingerprint inside the transaction. The
// caller decided against a snapshot it read earlier; this store rebuilds that
// snapshot from the committed rows and compares fingerprints, so a decision
// taken against a fleet that has since moved is refused rather than committed.
// Comparing fingerprints rather than re-running admission keeps the decision
// the caller's while making a stale one impossible to land. ledger.go owns that
// reconstruction, and its claim-set rule is exact: one ShareClaim per tenant
// recorded in the weight table, sorted, even at zero usage, while a tenant with
// usage but no weight is absent from Claims and still counted in Reserved.
//
// 4. A singleton fence-and-epoch row makes every mutation contend. Orchestration
// serialized on the row it was about to write; here there is one ledger row and
// every mutation -- including the two reads-that-write -- locks it first. That
// is deliberate: the fence is fleet-wide authority and the epoch is a global
// counter, and taking the lock in a consistent order first is what turns a
// deadlock into a queue. It also makes SQLSTATE 40001 routine rather than rare,
// which is why config.go re-argues the retry budget from scratch instead of
// inheriting orchestration's eight.
//
// 5. This store does not reproduce the memory adapter's record-count bound.
// That bound exists because the reference adapter is an in-process map that
// nothing evicts. Per the orchestration adapter: "refusing to record a stage
// because a table has many rows would turn a capacity signal into data loss."
// The same holds harder here -- refusing to record a reservation would leave
// the cluster holding capacity this ledger cannot see. Reads are still bounded:
// no query below can return an unbounded row count, and a list that overflows
// its bound is refused rather than truncated.
package schedulingpostgres

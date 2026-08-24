// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package orchestrationpostgres implements control/orchestration.Repository on
// PostgreSQL.
//
// The domain ships orchestration.MemoryRepository as its reference adapter, and
// docs/guides/go-service-golden-path.md forbids a production factory from
// falling back to it. This package is what makes that rule satisfiable: it is
// the durable implementation of the same interface, and it is written to agree
// with the reference adapter decision for decision. Where the two differ, the
// difference is deliberate and named in a comment.
//
// # What the interface demands and how it is met here
//
// Repository's contract is that every mutation is atomic with its audit record
// and its outbox append, in one transaction, with the resource-version
// precondition checked inside that transaction. Every mutation below therefore
// runs through runMutation, which opens exactly one SERIALIZABLE transaction,
// installs it on the context, and lets the audit recorder and outbox store join
// it through storage/sql/transaction. A precondition read is a locking read
// (FOR UPDATE) inside that same transaction, never a snapshot taken before it.
//
// Every mutation returns (value, replayed, error). Replay is decided from
// durable state, not from a cache: the row is read under a row lock and
// compared, so two deliveries of the same message converge whichever one
// commits first.
//
// TransitionStage puts a fourth write in that transaction when it promotes a
// stage to queued: the placement item, appended through the
// orchestration.Enqueuer a composition root binds with WithEnqueuer. It belongs
// there rather than after the commit because a promotion and its placement are
// one durable act -- a stage committed as queued whose work item was lost to a
// crash is work nothing will ever pick up, and an item committed before a
// transition that then rolled back is a placement for something that never
// became ready. A replayed transition returns before the append, so a
// redelivered reconcile places nothing.
//
// # Where this store deliberately differs from the memory adapter
//
// The memory adapter fails with a resource_exhausted "*_store_bound" fault once
// its map reaches MaximumMemoryRecords. That bound exists because the reference
// adapter is an in-process map that nothing evicts. A durable table is not that
// map, and refusing to record a stage because a table has many rows would turn
// a capacity signal into data loss. This store therefore does not reproduce the
// record-count bound. It does bound every read: no query here can return an
// unbounded row count, and a list that overflows its bound is refused rather
// than truncated, because a silently short stage list would read as a workflow
// with missing stages.
//
// # The stage version seal is called, never restated
//
// TransitionStage must produce the version the reference adapter would produce,
// or a record written by one adapter could not be transitioned by the other.
// This package once carried its own copy of that digest, because the rule lived
// in an unexported function; control/orchestration.SealStage is exported for
// exactly this reason, so stages.go calls it and the copy is gone. A second
// definition of a durable format is a fork waiting to happen, and a copy that
// only its own test consults cannot notice the fork it was written to catch.
// TestStageDigestMatchesTheMemoryAdapter is what keeps the agreement honest: it
// drives the same transitions through orchestration.MemoryRepository and
// through this store and compares the sealed versions, so a change that moved
// only one adapter fails a test rather than silently forking the format.
package orchestrationpostgres

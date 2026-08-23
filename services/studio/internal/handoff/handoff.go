// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package handoff implements the cross-surface handoff: an opaque,
// principal-bound, short-TTL handle redeemed server-side.
//
// # A pointer, not a capability
//
// Any surface — docs, a CLI, a finished CI job — POSTs to the machine plane
// with its bearer token and gets back https://mindclade.studio/o/<id>. A human
// clicks it, IAP establishes who they are, and the BFF RE-AUTHORIZES THAT HUMAN
// against the referenced resources before materializing anything.
//
// So a leaked link is inert without a session that already has access to the
// underlying resources. Nothing about what the handle points to is encoded in
// the URL, which is what keeps it out of access logs, Referer headers, and
// browser history in any useful form.
//
// # Idempotent for the binding principal, and that is required
//
// Strict single-use passes every security test and BREAKS THE BACK BUTTON:
// /o/<id> lands in history, and a back navigation re-requests it. So the first
// redemption binds the handle to its redeemer, and subsequent redemptions BY
// THAT PRINCIPAL within the TTL return the same 303 to the same document.
//
// The security property is unchanged — a leaked link is still inert for anyone
// else — and the browser works.
//
// # Why this is the one piece of server-side state the browser plane keeps
//
// A handle must be OPAQUE, which means the mapping to its resources lives
// somewhere the client cannot see. The session store was removed precisely
// because it put a stateful dependency on every browser request; this table is
// different in blast radius: if it is unavailable, handoff redemption fails and
// every other browser request keeps working. Nothing except /o/ should ever be
// on its path.
package handoff

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrNotFound covers every case a caller must not be able to distinguish:
	// no such handle, expired, or bound to somebody else.
	//
	// ONE error for all three on purpose. A 403 for "bound to somebody else"
	// would confirm the handle exists, which is exactly what an opaque handle
	// must not do.
	ErrNotFound = errors.New("handoff: no such handle")

	ErrCapExceeded = errors.New("handoff: creator has too many outstanding handles")

	// ErrContended means the bind lost its race maxBindAttempts times running.
	//
	// Distinct from ErrNotFound and from a database error on purpose: the
	// handle may well exist and the caller may well be entitled to it, so this
	// is "ask again", not "no". It is separate from a database failure because
	// the two want different responses — a 503 the client may retry, versus an
	// error somebody has to look at.
	ErrContended = errors.New("handoff: redemption lost its binding race repeatedly")
)

// DefaultTTL is short by design. Minutes, not hours: the handle is a pointer
// somebody is about to click, not a durable share.
const DefaultTTL = 15 * time.Minute

// DefaultOutstandingCap bounds unbound handles per creator. Without it,
// POST /v1/handoffs is an unbounded write from any bearer token.
const DefaultOutstandingCap = 100

// Handle is a created handoff.
type Handle struct {
	ID          string
	Creator     string
	ResourceRef json.RawMessage
	ExpiresAt   time.Time
}

// Redemption is the outcome of a successful redeem.
type Redemption struct {
	DocID string

	// FirstRedemption distinguishes the binding call from an idempotent repeat.
	// Only useful for the audit trail — both return the same 303.
	FirstRedemption bool
}

// Materializer turns a resource reference into a canvas document for a
// principal. Called only on the FIRST redemption.
type Materializer func(ctx context.Context, principal string, resourceRef json.RawMessage) (docID string, err error)

// Authorizer re-checks a principal's access to the referenced resources.
//
// Called on EVERY redemption, including the idempotent ones. Binding is not
// authorization — it only decides which document you get. Access revoked at
// minute one blocks the redemption at minute two.
type Authorizer func(ctx context.Context, principal string, resourceRef json.RawMessage) error

// Auditor records a redemption to an append-only sink.
//
// Append-only is the point: a trail the audited service can rewrite is
// decorative. In this estate that is a Cloud Logging bucket with retention lock,
// or a BigQuery dataset where this service holds insert but not delete or
// update.
type Auditor func(ctx context.Context, event AuditEvent)

// AuditEvent is what every redemption attempt records.
type AuditEvent struct {
	HandleID    string          `json:"handle_id"`
	Creator     string          `json:"creator_principal"`
	Redeemer    string          `json:"redeeming_principal"`
	ResourceRef json.RawMessage `json:"resource_ref"`
	DocID       string          `json:"doc_id,omitempty"`
	Outcome     string          `json:"outcome"`
	At          time.Time       `json:"at"`
}

// Store persists handles.
type Store struct {
	db             *sql.DB
	ttl            time.Duration
	outstandingCap int
	now            func() time.Time
}

func NewStore(db *sql.DB) *Store {
	return &Store{
		db:             db,
		ttl:            DefaultTTL,
		outstandingCap: DefaultOutstandingCap,
		now:            time.Now,
	}
}

// Create issues a handle.
func (s *Store) Create(ctx context.Context, creator string, resourceRef json.RawMessage) (Handle, error) {
	if creator == "" {
		return Handle{}, errors.New("handoff: creator is required")
	}

	// The partial index on unbound rows is what makes this count cheap — it
	// does not scan bound handles.
	var outstanding int
	const countQ = `
		SELECT count(*) FROM handoff_handles
		 WHERE creator_principal = $1 AND bound_principal IS NULL AND expires_at > now()`
	if err := s.db.QueryRowContext(ctx, countQ, creator).Scan(&outstanding); err != nil {
		return Handle{}, fmt.Errorf("handoff: count outstanding: %w", err)
	}
	if outstanding >= s.outstandingCap {
		return Handle{}, ErrCapExceeded
	}

	const insertQ = `
		INSERT INTO handoff_handles (id, creator_principal, resource_ref, expires_at)
		VALUES (gen_random_uuid(), $1, $2, $3)
		RETURNING id, expires_at`

	expiry := s.now().Add(s.ttl)
	var h Handle
	h.Creator = creator
	h.ResourceRef = resourceRef
	if err := s.db.QueryRowContext(ctx, insertQ, creator, []byte(resourceRef), expiry).
		Scan(&h.ID, &h.ExpiresAt); err != nil {
		return Handle{}, fmt.Errorf("handoff: create: %w", err)
	}
	return h, nil
}

// Redeem applies the three cases on bound_principal.
//
//	NULL              authorize, materialize, bind
//	equal to caller   return the stored doc_id
//	anything else     ErrNotFound
//
// Past expires_at, ErrNotFound for everyone including the binding principal.
func (s *Store) Redeem(
	ctx context.Context,
	handleID, principal string,
	authorize Authorizer,
	materialize Materializer,
	audit Auditor,
) (Redemption, error) {
	return s.redeem(ctx, handleID, principal,
		s.loadHandle(handleID), s.bindHandle(handleID, principal),
		authorize, materialize, audit)
}

// maxBindAttempts bounds the load-then-bind race.
//
// # Why this is not libs/go/retry
//
// Losing the bind is not a transient failure to wait out; it is a
// compare-and-set that lost, and the correct response is to re-read
// immediately and apply the three cases to what is actually there now. A
// backoff between attempts would only widen the window it is trying to close,
// so this stays a local CAS loop rather than a retry policy.
//
// # Why it has a bound, which it previously did not
//
// The original form re-entered Redeem by tail call with no attempt counter.
// Go does not eliminate tail calls, so a bind that keeps losing grows the
// goroutine stack until the runtime kills the ENTIRE PROCESS with a stack
// overflow — one contended handle taking down every unrelated request in the
// pod, which is a far worse outcome than failing the one redemption. Worse, it
// was silent until it was fatal: each lap also called materialize again, so a
// contended handle left an orphaned canvas document per lap on the way there.
//
// Three is what an honest race needs: the losing caller's second read sees the
// winner's binding and returns the idempotent redemption. Reaching the bound
// means the row is changing underneath every read, which is a condition to
// report rather than one to keep spinning on.
//
// This counts BINDS, not loads. The loop makes one more load than that, because
// it ends on a read rather than on a failed write — see the terminal branch for
// why giving up straight after a lost bind would answer the wrong question.
const maxBindAttempts = 3

// handleRow is one loaded handle, as the redeem loop needs it.
type handleRow struct {
	creator     string
	resourceRef []byte
	boundTo     string
	isBound     bool
	docID       string
}

// loadHandle and bindHandle are the two statements Redeem issues, returned as
// closures so the loop below can be driven without a live PostgreSQL.
//
// The seam is not decoration: every other test in this package skips without
// STUDIO_TEST_DATABASE_URL, which is exactly how an unbounded loop stayed here
// unnoticed. The bound is a property of the loop, so it is tested at the loop.
func (s *Store) loadHandle(handleID string) func(context.Context) (handleRow, error) {
	const loadQ = `
		SELECT creator_principal, resource_ref, bound_principal, doc_id
		  FROM handoff_handles
		 WHERE id = $1 AND expires_at > now()`

	return func(ctx context.Context) (handleRow, error) {
		var row handleRow
		var bound, docID sql.NullString

		err := s.db.QueryRowContext(ctx, loadQ, handleID).
			Scan(&row.creator, &row.resourceRef, &bound, &docID)
		if errors.Is(err, sql.ErrNoRows) {
			// Covers "no such handle" and "expired" identically — the caller
			// must not be able to tell them apart.
			return handleRow{}, ErrNotFound
		}
		if err != nil {
			return handleRow{}, fmt.Errorf("handoff: load %s: %w", handleID, err)
		}
		row.boundTo, row.isBound, row.docID = bound.String, bound.Valid, docID.String
		return row, nil
	}
}

// bindHandle reports ErrNotFound when the conditional update matched no row,
// which is the CAS having lost rather than the handle being absent.
func (s *Store) bindHandle(handleID, principal string) func(context.Context, string) (string, error) {
	// BINDING IS A CONDITIONAL UPDATE, not read-then-write. Two simultaneous
	// clicks — which is what a double-click on a link produces — would
	// otherwise both see NULL and both materialize a document.
	const bindQ = `
		UPDATE handoff_handles
		   SET bound_principal = $1, doc_id = $2
		 WHERE id = $3 AND bound_principal IS NULL AND expires_at > now()
		RETURNING doc_id`

	return func(ctx context.Context, docID string) (string, error) {
		var boundDoc string
		err := s.db.QueryRowContext(ctx, bindQ, principal, docID, handleID).Scan(&boundDoc)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		if err != nil {
			return "", fmt.Errorf("handoff: bind %s: %w", handleID, err)
		}
		return boundDoc, nil
	}
}

func (s *Store) redeem(
	ctx context.Context,
	handleID, principal string,
	load func(context.Context) (handleRow, error),
	bind func(context.Context, string) (string, error),
	authorize Authorizer,
	materialize Materializer,
	audit Auditor,
) (Redemption, error) {
	// materialize runs AT MOST ONCE per call, and the document it produced is
	// re-offered to every subsequent bind.
	//
	// Re-materializing per lap — which the recursive form did — leaves a real
	// canvas document behind on every lost race, referenced by nothing and
	// cleaned up by nothing. Re-offering is safe precisely because the bind is
	// a compare-and-set: the same document either takes the row or it does not,
	// and it was materialized for this principal from this resource_ref either
	// way. Bounding the laps caps that leak; hoisting removes it.
	var proposedDocID string
	var proposed bool

	for attempt := 0; ; attempt++ {
		// Cancellation is checked per lap. Without it a caller that has already
		// gone away still pays for another load, another authorize, and another
		// materialize before the statements themselves notice.
		if err := ctx.Err(); err != nil {
			return Redemption{}, err
		}

		row, err := load(ctx)
		if err != nil {
			return Redemption{}, err
		}

		// Bound to someone else: indistinguishable from not existing. Audited,
		// because an attempt on a handle bound to another principal is exactly
		// the event a trail exists to record.
		if row.isBound && row.boundTo != principal {
			s.record(ctx, audit, AuditEvent{
				HandleID: handleID, Creator: row.creator, Redeemer: principal,
				ResourceRef: row.resourceRef, Outcome: "denied_bound_elsewhere", At: s.now(),
			})
			return Redemption{}, ErrNotFound
		}

		// RE-AUTHORIZE ON EVERY REDEMPTION, including the idempotent repeat
		// below.
		if err := authorize(ctx, principal, row.resourceRef); err != nil {
			s.record(ctx, audit, AuditEvent{
				HandleID: handleID, Creator: row.creator, Redeemer: principal,
				ResourceRef: row.resourceRef, Outcome: "denied_unauthorized", At: s.now(),
			})
			return Redemption{}, ErrNotFound
		}

		// The back-button case: same principal, within TTL, same document.
		if row.isBound {
			s.record(ctx, audit, AuditEvent{
				HandleID: handleID, Creator: row.creator, Redeemer: principal,
				ResourceRef: row.resourceRef, DocID: row.docID,
				Outcome: "redeemed_idempotent", At: s.now(),
			})
			return Redemption{DocID: row.docID}, nil
		}

		// THE BIND ATTEMPTS ARE SPENT, and the load above was the reconciling
		// read.
		//
		// Giving up straight after the last lost bind would be wrong in both
		// directions, because a lost bind means the row IS bound now — quite
		// possibly to this same principal, which is the double-click this loop
		// exists to serve. Ending on a load instead means the three cases above
		// have already answered: the caller gets their 303 if the winner was
		// them, a 404 if the handle expired underneath, and only a genuinely
		// still-unbound row reaches here.
		if attempt == maxBindAttempts {
			// Audited like every other terminal branch. This is the one that
			// can have materialized a document, so a trail without it leaves
			// nothing to join a burst of 503s to.
			s.record(ctx, audit, AuditEvent{
				HandleID: handleID, Creator: row.creator, Redeemer: principal,
				ResourceRef: row.resourceRef, DocID: proposedDocID,
				Outcome: "contended", At: s.now(),
			})
			return Redemption{}, ErrContended
		}

		if !proposed {
			proposedDocID, err = materialize(ctx, principal, row.resourceRef)
			if err != nil {
				return Redemption{}, fmt.Errorf("handoff: materialize for %s: %w", handleID, err)
			}
			proposed = true
		}

		boundDoc, err := bind(ctx, proposedDocID)
		if errors.Is(err, ErrNotFound) {
			// Someone else won the race, or it expired between the load and
			// here. Re-read and apply the three cases again rather than
			// guessing — the next lap returns the winner's document.
			continue
		}
		if err != nil {
			return Redemption{}, err
		}

		s.record(ctx, audit, AuditEvent{
			HandleID: handleID, Creator: row.creator, Redeemer: principal,
			ResourceRef: row.resourceRef, DocID: boundDoc,
			Outcome: "redeemed_first", At: s.now(),
		})
		return Redemption{DocID: boundDoc, FirstRedemption: true}, nil
	}
}

func (s *Store) record(ctx context.Context, audit Auditor, e AuditEvent) {
	if audit == nil {
		return
	}
	audit(ctx, e)
}

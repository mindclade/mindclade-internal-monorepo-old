// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

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
	const loadQ = `
		SELECT creator_principal, resource_ref, bound_principal, doc_id
		  FROM handoff_handles
		 WHERE id = $1 AND expires_at > now()`

	var creator string
	var resourceRef []byte
	var bound sql.NullString
	var docID sql.NullString

	err := s.db.QueryRowContext(ctx, loadQ, handleID).Scan(&creator, &resourceRef, &bound, &docID)
	if errors.Is(err, sql.ErrNoRows) {
		// Covers "no such handle" and "expired" identically — the caller must
		// not be able to tell them apart.
		return Redemption{}, ErrNotFound
	}
	if err != nil {
		return Redemption{}, fmt.Errorf("handoff: load %s: %w", handleID, err)
	}

	// Bound to someone else: indistinguishable from not existing. Audited,
	// because an attempt on a handle bound to another principal is exactly the
	// event a trail exists to record.
	if bound.Valid && bound.String != principal {
		s.record(ctx, audit, AuditEvent{
			HandleID: handleID, Creator: creator, Redeemer: principal,
			ResourceRef: resourceRef, Outcome: "denied_bound_elsewhere", At: s.now(),
		})
		return Redemption{}, ErrNotFound
	}

	// RE-AUTHORIZE ON EVERY REDEMPTION, including the idempotent repeat below.
	if err := authorize(ctx, principal, resourceRef); err != nil {
		s.record(ctx, audit, AuditEvent{
			HandleID: handleID, Creator: creator, Redeemer: principal,
			ResourceRef: resourceRef, Outcome: "denied_unauthorized", At: s.now(),
		})
		return Redemption{}, ErrNotFound
	}

	// The back-button case: same principal, within TTL, same document.
	if bound.Valid {
		s.record(ctx, audit, AuditEvent{
			HandleID: handleID, Creator: creator, Redeemer: principal,
			ResourceRef: resourceRef, DocID: docID.String,
			Outcome: "redeemed_idempotent", At: s.now(),
		})
		return Redemption{DocID: docID.String}, nil
	}

	newDocID, err := materialize(ctx, principal, resourceRef)
	if err != nil {
		return Redemption{}, fmt.Errorf("handoff: materialize for %s: %w", handleID, err)
	}

	// BINDING IS A CONDITIONAL UPDATE, not read-then-write. Two simultaneous
	// clicks — which is what a double-click on a link produces — would
	// otherwise both see NULL and both materialize a document.
	const bindQ = `
		UPDATE handoff_handles
		   SET bound_principal = $1, doc_id = $2
		 WHERE id = $3 AND bound_principal IS NULL AND expires_at > now()
		RETURNING doc_id`

	var boundDoc string
	err = s.db.QueryRowContext(ctx, bindQ, principal, newDocID, handleID).Scan(&boundDoc)
	if errors.Is(err, sql.ErrNoRows) {
		// Someone else won the race, or it expired between the load and here.
		// Re-read and apply the three cases again rather than guessing.
		return s.Redeem(ctx, handleID, principal, authorize, materialize, audit)
	}
	if err != nil {
		return Redemption{}, fmt.Errorf("handoff: bind %s: %w", handleID, err)
	}

	s.record(ctx, audit, AuditEvent{
		HandleID: handleID, Creator: creator, Redeemer: principal,
		ResourceRef: resourceRef, DocID: boundDoc,
		Outcome: "redeemed_first", At: s.now(),
	})
	return Redemption{DocID: boundDoc, FirstRedemption: true}, nil
}

func (s *Store) record(ctx context.Context, audit Auditor, e AuditEvent) {
	if audit == nil {
		return
	}
	audit(ctx, e)
}

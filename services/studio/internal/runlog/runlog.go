// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

// Package runlog reads and writes the durable, ordered log that every stream is
// a cursor over.
//
// # The stream is a cursor, not a pipe
//
// The naive design treats the SSE connection as the run: the client connects,
// events flow, and if the connection dies the output is gone from the client's
// point of view. That version needs an hour-long backend timeout to be CORRECT,
// and it still loses on every rolling deploy.
//
// Inverted, the run appends to this log — which is the source of truth — and
// the SSE connection is a cursor over it. Severance by deploy, timeout, or
// network blip becomes a normal event the client recovers from with
// Last-Event-ID, never a loss of work.
//
// What that buys, in one mechanism: rolling deploys stop severing work, the
// backend timeout drops from a correctness parameter to a hygiene bound,
// network blips self-heal, and terminationGracePeriodSeconds no longer has to
// exceed the longest possible run.
//
// # Replay is a range scan, never an in-process buffer
//
// An in-process buffer fails exactly when it is needed — when the pod holding
// it went away. Because the log is the state, every streaming replica is
// stateless and interchangeable: any replica can serve any cursor for any run.
// There is no session affinity to configure and no reason a resume must land on
// the same pod.
//
// # Heartbeats are not events
//
// A `: ping` comment carries no id and is never written here. A heartbeat that
// advances the cursor makes resume skip real output.
package runlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// TerminalEventType tells the client to stop reconnecting.
//
// Its ABSENCE is a specific, nasty failure: a completed run produces an
// infinite reconnect loop against a stream that will never send anything again.
// The executor must append one on cancellation and failure exactly as it does
// on success — the cancellation path is the one nobody tests.
const TerminalEventType = "done"

// MaxPayloadBytes matches the CHECK constraint on the table. Structures,
// trajectories, and any model artifact go to object storage with a POINTER in
// the event: a transcript row is a reference to a result, not the result.
const MaxPayloadBytes = 64 * 1024

var (
	ErrPayloadTooLarge = errors.New("runlog: payload exceeds 64KiB; store the artifact and reference it")
	ErrNotFound        = errors.New("runlog: run not found")
)

// Event is one row of the log.
type Event struct {
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// Terminal reports whether this event ends the stream.
func (e Event) Terminal() bool { return e.Type == TerminalEventType }

// Store reads and appends run events.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Append writes one event, allocating seq from the run's current maximum.
//
// # Sequence allocation is per-run, never global
//
// A shared sequence leaves gaps on transaction rollback, and a gap breaks
// density — which breaks resume, because the client cannot distinguish "no
// event yet" from "event lost".
//
// # The run row's lock is required, not belt-and-braces
//
// A single `INSERT … SELECT COALESCE(MAX(seq),0)+1 …` statement looks atomic
// and is not. At READ COMMITTED — PostgreSQL's default — two concurrent
// appends both read the same maximum and both insert the same seq. The
// duplicate is not even caught by the primary key, because the table is
// partitioned by created_at and the key must therefore include it, which makes
// uniqueness per-partition rather than global.
//
// That combination is worth stating plainly: the obvious statement, on the
// obvious schema, silently produces duplicate sequence numbers under
// concurrency. A test that appends sequentially never sees it.
//
// So the allocation happens under `SELECT … FOR UPDATE` on the run row, which
// serialises appends for one run while leaving different runs fully parallel.
// The cost is one extra round trip per event.
//
// One writer per run remains the design's assumption. This is what makes the
// assumption safe to hold rather than merely stated — assert it, but do not
// rely on it for correctness of the sequence.
func (s *Store) Append(ctx context.Context, runID string, eventType string, payload json.RawMessage) (int64, error) {
	if len(payload) > MaxPayloadBytes {
		return 0, fmt.Errorf("%w: %d bytes", ErrPayloadTooLarge, len(payload))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("runlog: begin append to %s: %w", runID, err)
	}
	defer func() { _ = tx.Rollback() }()

	// Serialises concurrent appends for THIS run only. Appends to other runs
	// take different row locks and do not contend.
	var exists string
	err = tx.QueryRowContext(ctx, `SELECT id FROM runs WHERE id = $1 FOR UPDATE`, runID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("runlog: lock run %s: %w", runID, err)
	}

	const q = `
		INSERT INTO run_events (run_id, seq, event_type, payload)
		SELECT $1, COALESCE(MAX(seq), 0) + 1, $2, $3
		  FROM run_events
		 WHERE run_id = $1
		RETURNING seq`

	var seq int64
	if err := tx.QueryRowContext(ctx, q, runID, eventType, []byte(payload)).Scan(&seq); err != nil {
		return 0, fmt.Errorf("runlog: append to %s: %w", runID, err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("runlog: commit append to %s: %w", runID, err)
	}
	return seq, nil
}

// ReadFrom returns events after the given cursor, in order.
//
// # startedAt is not optional, and the reason is not obvious
//
// `WHERE run_id = $1 AND seq > $2` filters on NEITHER partition key, so the
// planner probes EVERY live partition's index — thirty of them at daily
// granularity with 30-day retention. That is survivable and pointless, and it
// degrades linearly as retention grows.
//
// The run's start time is already known to the caller, so passing it costs one
// parameter and turns thirty partition probes into one or two. This is the kind
// of detail that looks like premature optimisation at ten runs and like an
// incident at ten thousand.
func (s *Store) ReadFrom(ctx context.Context, runID string, afterSeq int64, startedAt time.Time, limit int) ([]Event, error) {
	const q = `
		SELECT seq, event_type, payload, created_at
		  FROM run_events
		 WHERE run_id = $1
		   AND seq > $2
		   AND created_at >= $3
		 ORDER BY seq
		 LIMIT $4`

	// A day of slack before the run's recorded start, so a clock skew between
	// the submitting and executing services cannot exclude the run's own first
	// events from the scan.
	lowerBound := startedAt.Add(-24 * time.Hour)

	rows, err := s.db.QueryContext(ctx, q, runID, afterSeq, lowerBound, limit)
	if err != nil {
		return nil, fmt.Errorf("runlog: read %s from %d: %w", runID, afterSeq, err)
	}
	defer func() { _ = rows.Close() }()

	var events []Event
	for rows.Next() {
		var e Event
		var payload []byte
		if err := rows.Scan(&e.Seq, &e.Type, &payload, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("runlog: scan: %w", err)
		}
		e.Payload = json.RawMessage(payload)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runlog: iterate: %w", err)
	}
	return events, nil
}

// RunMeta is what a streaming handler needs before it can serve a cursor.
type RunMeta struct {
	ID          string
	Submitter   string
	StartedAt   time.Time
	CancelledAt *time.Time
}

// Run loads a run's metadata.
func (s *Store) Run(ctx context.Context, runID string) (RunMeta, error) {
	const q = `SELECT id, submitter, started_at, cancel_requested_at FROM runs WHERE id = $1`

	var m RunMeta
	var cancelled sql.NullTime
	err := s.db.QueryRowContext(ctx, q, runID).Scan(&m.ID, &m.Submitter, &m.StartedAt, &cancelled)
	if errors.Is(err, sql.ErrNoRows) {
		return RunMeta{}, ErrNotFound
	}
	if err != nil {
		return RunMeta{}, fmt.Errorf("runlog: load run %s: %w", runID, err)
	}
	if cancelled.Valid {
		m.CancelledAt = &cancelled.Time
	}
	return m, nil
}

// RequestCancel sets the durable cancellation flag.
//
// # Cancellation crosses no planes
//
// The alternative — the BFF calling the executor over ClusterIP — adds a
// synchronous cross-plane control path, fails when the executor is mid-restart,
// and opens a trust edge to carry one boolean. The executor already writes to
// this database on every event, so it checks the flag between checkpoints at no
// additional cost, and the request survives an executor restart because it is
// durable rather than in flight.
//
// Reports whether this call was the one that set it, so a second cancel is a
// 409 rather than a silent success.
func (s *Store) RequestCancel(ctx context.Context, runID string) (bool, error) {
	const q = `
		UPDATE runs SET cancel_requested_at = now()
		 WHERE id = $1 AND cancel_requested_at IS NULL`

	res, err := s.db.ExecContext(ctx, q, runID)
	if err != nil {
		return false, fmt.Errorf("runlog: cancel %s: %w", runID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("runlog: cancel %s: %w", runID, err)
	}
	return n == 1, nil
}

// Submit creates a run idempotently.
//
// # Idempotency is the unique constraint, not a lookup
//
// Read-then-insert instead, and two concurrent retries both see nothing and
// both submit — which is exactly what a flaky connection produces, and a
// duplicate GPU job is a real cost rather than a cosmetic one.
//
// Returns the run id and whether this call created it. A replay with a MATCHING
// digest returns the original id and created=false, which the handler turns
// into a 202; a replay with a DIFFERENT digest returns ErrDigestMismatch,
// which becomes a 409.
func (s *Store) Submit(ctx context.Context, runID, submitter, idempotencyKey, digest string) (string, bool, error) {
	const insert = `
		INSERT INTO runs (id, submitter, idempotency_key, request_digest)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (submitter, idempotency_key) DO NOTHING
		RETURNING id`

	var id string
	err := s.db.QueryRowContext(ctx, insert, runID, submitter, idempotencyKey, digest).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("runlog: submit: %w", err)
	}

	// Zero rows: the key was seen before. Read the existing row and compare.
	const existing = `
		SELECT id, request_digest FROM runs
		 WHERE submitter = $1 AND idempotency_key = $2`

	var storedDigest string
	if err := s.db.QueryRowContext(ctx, existing, submitter, idempotencyKey).Scan(&id, &storedDigest); err != nil {
		return "", false, fmt.Errorf("runlog: read existing submission: %w", err)
	}
	if storedDigest != digest {
		return "", false, ErrDigestMismatch
	}
	return id, false, nil
}

// ErrDigestMismatch means the same key was reused with a different body.
var ErrDigestMismatch = errors.New("runlog: idempotency key reused with a different request body")

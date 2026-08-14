-- Copyright 2026 Mindclade. All rights reserved.
-- Confidential, proprietary, and trade-secret information.
--
-- The browser plane's durable state: runs, their transcripts, their results, canvas
-- documents, and handoff handles.
--
-- ONE datastore for all five, deliberately. Three of them were candidates for separate
-- stores; keeping them together buys one backup posture, one connection pool to reason
-- about, and one availability dependency instead of three with three failure modes. The
-- team is small and that trade is the right way round.
--
-- ===========================================================================================
-- What is NOT here, and why
-- ===========================================================================================
-- No session table. The session is a five-minute AEAD cookie bound to the IAP subject, so
-- there is nothing server-side to store. A session store would have made a stateful service a
-- hard dependency of EVERY browser request in the system while only caching a decision — and
-- the handles table below shows the contrast: if IT is unavailable, handoff redemption fails
-- and every other browser request keeps working.

BEGIN;

-- ===========================================================================================
-- runs
-- ===========================================================================================
CREATE TABLE runs (
    id                  uuid        PRIMARY KEY,
    submitter           text        NOT NULL,

    -- IDEMPOTENCY IS THIS CONSTRAINT, not a lookup.
    --
    -- Submission is one statement:
    --
    --   INSERT INTO runs (id, submitter, idempotency_key, request_digest)
    --   VALUES ($1, $2, $3, $4)
    --   ON CONFLICT (submitter, idempotency_key) DO NOTHING
    --   RETURNING id;
    --
    -- Zero rows means the key was seen before: read the existing row and return its id with
    -- 202 if request_digest matches, 409 if it does not.
    --
    -- Implemented as read-then-insert instead, two concurrent retries both see nothing and
    -- both submit — which is exactly what a flaky connection produces, and a duplicate GPU job
    -- is a real cost rather than a cosmetic one. The uniqueness is scoped to the SUBMITTER so
    -- that two principals choosing the same key do not collide.
    idempotency_key     text        NOT NULL,
    request_digest      text        NOT NULL,

    -- CANCELLATION IS A COLUMN, not a call.
    --
    -- The alternative — the BFF calling the executor over ClusterIP — adds a synchronous
    -- cross-plane control path, fails when the executor is mid-restart, and opens a trust edge
    -- to carry one boolean. The executor already writes to this database on every event, so it
    -- checks this flag between checkpoints at no additional cost, and the request survives an
    -- executor restart because it is durable rather than in flight.
    --
    -- The executor MUST then append a terminal event exactly as it does on success. Skipping
    -- it leaves every attached client reconnecting forever against a log that will never grow
    -- again.
    cancel_requested_at timestamptz,

    started_at          timestamptz NOT NULL DEFAULT now(),
    created_at          timestamptz NOT NULL DEFAULT now(),

    UNIQUE (submitter, idempotency_key)
);

-- ===========================================================================================
-- run_events — the durable log every stream is a cursor over
-- ===========================================================================================
-- This is the table D13 rests on. The SSE connection is not the run; the log is. A severed
-- connection is a normal event the client recovers from, not a loss of work.
--
-- Requirements, and what breaks without each:
--
--   append-only, one writer per run   No coordination needed. Assert it rather than assume it.
--   dense, monotonic seq per run      Gaps break resume — the client cannot tell "no event
--                                     yet" from "event lost".
--   random access from an offset      This IS the resume operation: a range scan, not a
--                                     subscription.
--   survives losing the writing pod   The whole point. An in-process buffer fails exactly
--                                     when it is needed, which is when that pod went away.
--
-- Why a relational table and not something more obviously suited:
--
--   NOT an in-process ring buffer.  Passes every test that does not kill a pod.
--   NOT Pub/Sub.                    Subscriptions are not random-access by offset.
--                                   Seek-to-timestamp is a different operation.
--   NOT Redis Streams.              Fits the access pattern, but puts a second in-memory store
--                                   on the critical path right after the session store was
--                                   removed. If it is unavailable, in-flight runs lose their
--                                   transcript — worse than losing sessions, because a run
--                                   cannot be re-derived by asking the user to sign in again.
--
-- The resume query is an index-only range scan on the primary key. The access pattern and the
-- storage layout are the same shape, which is the actual argument.
CREATE TABLE run_events (
    run_id      uuid        NOT NULL,

    -- Allocated PER RUN, never from a global sequence. A shared sequence leaves gaps on
    -- transaction rollback, and a gap breaks density — which breaks resume. Allocate in the
    -- single writer for that run.
    seq         bigint      NOT NULL,

    event_type  text        NOT NULL,
    payload     jsonb       NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),

    -- created_at is in the key because PostgreSQL REQUIRES it: a unique constraint on a
    -- partitioned table must contain every partitioning column. `PRIMARY KEY (run_id, seq)`
    -- is rejected outright with "lacks column created_at which is part of the partition key",
    -- and only at apply — it looks correct until then.
    --
    -- The consequence has to be stated rather than glossed: uniqueness of (run_id, seq) is
    -- enforced PER PARTITION, not globally. A run whose events straddle midnight could in
    -- principle carry the same seq twice, in two partitions, and the constraint would not
    -- catch it.
    --
    -- What actually guarantees density is the SINGLE WRITER allocating seq from the run's
    -- current maximum — the constraint was never the mechanism, only a backstop. This
    -- narrows the backstop; it does not remove the guarantee. Assert one writer per run
    -- rather than assuming it.
    --
    -- Ordering is unaffected: (run_id, seq) are the leading columns, so the resume query is
    -- still an index-only range scan.
    PRIMARY KEY (run_id, seq, created_at),

    -- 64 KB is generous for a transcript row. Structures, trajectories, and any model artifact
    -- go to object storage with a POINTER in the event: a transcript row is a reference to a
    -- result, not the result. Without this the events table becomes the largest object in the
    -- database and 30-day retention stops being affordable.
    CONSTRAINT run_events_payload_size CHECK (pg_column_size(payload) <= 65536)
) PARTITION BY RANGE (created_at);

-- One partition per day. Retention is then a partition DROP rather than a delete sweep, which
-- is what makes 30-day retention cheap instead of a nightly vacuum problem.
--
-- These are created ahead of time by a scheduled job; the two below exist so a fresh database
-- accepts writes immediately. A missing partition is an INSERT error naming no partition
-- found for the row — legible, but only if you know to look here.
CREATE TABLE run_events_default PARTITION OF run_events DEFAULT;

-- ===========================================================================================
-- run_results — separate from the transcript, deliberately
-- ===========================================================================================
-- The transcript is how a run is replayed in the UI. It is NOT the system of record for the
-- run's output. Conflating the two is how the events table becomes the largest object in the
-- database, and it is why 30-day event retention can never delete an output.
CREATE TABLE run_results (
    run_id       uuid        PRIMARY KEY REFERENCES runs(id),
    status       text        NOT NULL CHECK (status IN ('succeeded', 'failed', 'cancelled')),

    -- An object-storage URI. Artifacts are never inlined — see the payload cap above.
    result_ref   text,
    completed_at timestamptz
);

-- ===========================================================================================
-- canvas_docs
-- ===========================================================================================
-- The only irreplaceable data in this schema. Point-in-time recovery is on for it; a lost
-- transcript is not a lost result, but a lost document is a lost document.
CREATE TABLE canvas_docs (
    id         uuid        PRIMARY KEY,
    owner      text        NOT NULL,

    -- What the ETag carries and what If-Match asserts.
    version    bigint      NOT NULL DEFAULT 1,
    body       jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- The update is ONE statement, and ZERO ROWS RETURNED IS THE 409:
--
--   UPDATE canvas_docs
--      SET body = $1, version = version + 1, updated_at = now()
--    WHERE id = $2 AND version = $3
--   RETURNING version;
--
-- There is no read-then-compare, so there is no window between the check and the write.
-- Implemented as read-then-write instead, this is a lost-update bug that appears only under
-- concurrency and therefore not in testing. Return the current row in the 409 body so the
-- client can diff without a second round trip.

CREATE INDEX canvas_docs_owner_idx ON canvas_docs (owner);

-- ===========================================================================================
-- handoff_handles
-- ===========================================================================================
-- The ONE piece of server-side state the browser plane keeps. A handle must be opaque, which
-- means the mapping to its resources lives somewhere the client cannot see.
--
-- Its blast radius is deliberately small: short-TTL, low-volume, and if this table is
-- unavailable, handoff redemption fails while every other browser request continues working.
-- That is the distinction from a session store, which would have taken the whole plane down.
-- Nothing except /o/ should ever be on this table's path.
CREATE TABLE handoff_handles (
    id                uuid        PRIMARY KEY,
    creator_principal text        NOT NULL,
    resource_ref      jsonb       NOT NULL,

    -- NULL until first redemption. The three redemption cases are exactly three states of
    -- this column: NULL -> authorize, materialize, bind; equal to the caller -> return the
    -- stored doc_id; anything else -> 404.
    --
    -- 404 and never 403: a 403 confirms the handle exists.
    bound_principal   text,
    doc_id            uuid,

    expires_at        timestamptz NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),

    -- doc_id is set WITH bound_principal or not at all. A bound handle with no document is a
    -- redemption that half-succeeded, and the client would get a 303 to nowhere.
    CONSTRAINT handoff_binding_is_atomic
        CHECK ((bound_principal IS NULL) = (doc_id IS NULL))
);

-- Binding MUST be a conditional update, not read-then-write:
--
--   UPDATE handoff_handles
--      SET bound_principal = $1, doc_id = $2
--    WHERE id = $3 AND bound_principal IS NULL AND expires_at > now()
--   RETURNING doc_id;
--
-- Two simultaneous clicks — which is what a double-click on a link produces — would otherwise
-- both see NULL and both materialize a document. Zero rows means someone else won or the
-- handle is bound: re-read and apply the three cases.
--
-- Re-authorization runs on EVERY redemption, including the idempotent ones. Binding is not
-- authorization; it only decides which document you get.

-- Partial index over UNBOUND handles only. It supports the per-creator outstanding-handle cap
-- without scanning bound rows — POST /v1/handoffs is otherwise an unbounded write from any
-- bearer token.
CREATE INDEX handoff_handles_unbound_by_creator_idx
    ON handoff_handles (creator_principal)
    WHERE bound_principal IS NULL;

-- Expiry sweep support. Past expires_at a handle is 404 for everyone, but the rows still need
-- collecting.
CREATE INDEX handoff_handles_expires_at_idx ON handoff_handles (expires_at);

COMMIT;

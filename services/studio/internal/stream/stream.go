// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package stream serves a run's log as server-sent events.
//
// # SSE, not WebSocket
//
// Plain HTTP/2 with no protocol upgrade through the load balancer, and
// same-origin means cookies ride along natively. Model runs are
// server-dominant, so full duplex buys nothing. A WebSocket endpoint may be
// worth adding later for presence and cursors, if ever.
//
// # The client is fetch(), not EventSource
//
// EventSource is the obvious choice and the wrong one, for one reason: it
// cannot see a response status. When the IAP session expires mid-run, the
// reconnect gets a 302 to Google's sign-in page; EventSource sees only "not a
// valid event stream", fires onerror, and retries — forever, against a redirect
// that will never become a stream. The user watches a run that has silently
// stopped, and there is no client-side fix, because the API exposes neither
// status nor headers.
//
// This handler is therefore written to be consumed by fetch() with a
// ReadableStream: it returns real status codes on the auth paths and reads
// Last-Event-ID from the HEADER rather than relying on EventSource's automatic
// resend.
//
// # Heartbeats carry no id
//
// A `: ping` comment every ~15s stops an idle connection being timed out. It is
// a COMMENT, not an event: it carries no id and is never written to the log. A
// heartbeat that advances the cursor makes resume skip real output.
package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"mindclade.internal/services/studio/internal/runlog"
)

const (
	// HeartbeatInterval must stay comfortably under any idle timeout on the
	// path — the load balancer's, and any proxy in front of it in development.
	HeartbeatInterval = 15 * time.Second

	// PollInterval is how often the cursor looks for new events. This is a
	// tail over a table, not a subscription: simple, and adequate because the
	// alternative (LISTEN/NOTIFY) pins a connection per stream to deliver a
	// wake-up that a 250ms poll already provides.
	PollInterval = 250 * time.Millisecond

	// BatchLimit caps one read. A run that produces faster than a client
	// consumes simply leaves the cursor behind and the client catches up on its
	// next read — backpressure needs no mechanism in a cursor model, which is
	// another reason it beats a socket.
	BatchLimit = 500
)

// Reader is the subset of the run log this handler needs.
type Reader interface {
	Run(ctx context.Context, runID string) (runlog.RunMeta, error)
	ReadFrom(ctx context.Context, runID string, afterSeq int64, startedAt time.Time, limit int) ([]runlog.Event, error)
}

// Authorizer decides whether a principal may read a run.
//
// Re-checked on every connection, including every resume. A reconnect is a new
// request and gets a new authorization decision; access revoked at minute one
// blocks the resume at minute two.
type Authorizer func(ctx context.Context, principal string, run runlog.RunMeta) error

// Handler serves GET /api/stream/runs/{runID}.
type Handler struct {
	Reader      Reader
	Authorize   Authorizer
	Logger      *slog.Logger
	Heartbeat   time.Duration
	Poll        time.Duration
	principalOf func(*http.Request) (string, bool)
}

// New builds a Handler. principalOf extracts the authenticated principal — it
// comes from the session middleware, which has already verified the IAP
// assertion and checked that the session is bound to the same subject.
func New(r Reader, authorize Authorizer, principalOf func(*http.Request) (string, bool), logger *slog.Logger) *Handler {
	return &Handler{
		Reader:      r,
		Authorize:   authorize,
		Logger:      logger,
		Heartbeat:   HeartbeatInterval,
		Poll:        PollInterval,
		principalOf: principalOf,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("runID")
	if runID == "" {
		http.Error(w, "missing run id", http.StatusBadRequest)
		return
	}

	principal, ok := h.principalOf(r)
	if !ok {
		// A STATUS, never an HTML body. A 200 carrying HTML here is
		// indistinguishable from a stream to a client that cannot read status
		// codes — which is exactly the failure that motivated consuming this
		// with fetch() rather than EventSource.
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}

	run, err := h.Reader.Run(r.Context(), runID)
	if errors.Is(err, runlog.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		h.Logger.Error("load run", "run_id", runID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := h.Authorize(r.Context(), principal, run); err != nil {
		// 404, not 403. A 403 confirms the run exists.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	cursor, err := parseCursor(r)
	if err != nil {
		http.Error(w, "invalid Last-Event-ID", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		// Without flushing, every event buffers until the handler returns —
		// which for a live run is never.
		h.Logger.Error("response writer cannot flush; streaming is impossible")
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	// Defeats proxy buffering, which otherwise holds events until a buffer
	// fills and makes a working stream look like a hung one.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	h.pump(r.Context(), w, flusher, run, cursor)
}

func (h *Handler) pump(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, run runlog.RunMeta, cursor int64) {
	heartbeat := time.NewTicker(h.Heartbeat)
	defer heartbeat.Stop()
	poll := time.NewTicker(h.Poll)
	defer poll.Stop()

	for {
		events, err := h.Reader.ReadFrom(ctx, run.ID, cursor, run.StartedAt, BatchLimit)
		if err != nil {
			if ctx.Err() != nil {
				return // the client went away; not an error
			}
			h.Logger.Error("read run log", "run_id", run.ID, "cursor", cursor, "error", err)
			// The status is already sent, so the only honest signal left is to
			// end the response. The client sees a clean EOF and resumes from
			// its own cursor, which is the same path a rolling deploy takes.
			return
		}

		for _, e := range events {
			if err := writeEvent(w, e); err != nil {
				return
			}
			cursor = e.Seq
			flusher.Flush()

			// The terminal event ends the stream. Without it, a completed run
			// produces an infinite reconnect loop against a log that will
			// never grow again.
			if e.Terminal() {
				return
			}
		}

		// More waiting: drain before idling.
		if len(events) == BatchLimit {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			// A COMMENT. No id: field, so it cannot advance the cursor.
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-poll.C:
		}
	}
}

// writeEvent emits one SSE frame.
//
// The id: field is what the client echoes back as Last-Event-ID, and it is the
// event's seq — dense and per-run, so the client can tell "no event yet" from
// "event lost".
func writeEvent(w http.ResponseWriter, e runlog.Event) error {
	payload := e.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}

	// data: must be a single line. Payloads are JSON, which never contains a
	// literal newline outside a string, and Go's encoder escapes those — but a
	// hand-built payload could, and a newline mid-frame silently truncates the
	// event to whatever preceded it.
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, payload); err != nil {
		return err
	}
	return nil
}

// parseCursor reads Last-Event-ID.
//
// Absent means start from zero — a fresh connection replays the run from the
// beginning, which is what makes opening a completed run in a new tab work.
//
// The HEADER, not a query parameter: with fetch() the client sets it
// explicitly, and keeping it in the header means a resumed URL is identical to
// a fresh one, so nothing leaks a cursor into logs or browser history.
func parseCursor(r *http.Request) (int64, error) {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		return 0, nil
	}
	seq, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seq < 0 {
		return 0, fmt.Errorf("stream: invalid Last-Event-ID %q", raw)
	}
	return seq, nil
}

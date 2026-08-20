// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package stream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mindclade.dev/services/studio/internal/runlog"
)

// fakeReader is an in-memory log. Events can be appended while a stream is
// running, which is how the tail and resume paths are exercised.
type fakeReader struct {
	mu     sync.Mutex
	meta   runlog.RunMeta
	events []runlog.Event
	err    error
}

func newFakeReader() *fakeReader {
	return &fakeReader{
		meta: runlog.RunMeta{
			ID:        "run-1",
			Submitter: "alice",
			StartedAt: time.Now().Add(-time.Minute),
		},
	}
}

func (f *fakeReader) Run(_ context.Context, runID string) (runlog.RunMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if runID != f.meta.ID {
		return runlog.RunMeta{}, runlog.ErrNotFound
	}
	return f.meta, nil
}

func (f *fakeReader) ReadFrom(_ context.Context, _ string, afterSeq int64, _ time.Time, limit int) ([]runlog.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	var out []runlog.Event
	for _, e := range f.events {
		if e.Seq > afterSeq {
			out = append(out, e)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeReader) append(seq int64, eventType, payload string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, runlog.Event{
		Seq: seq, Type: eventType,
		Payload: json.RawMessage(payload), CreatedAt: time.Now(),
	})
}

func allow(context.Context, string, runlog.RunMeta) error { return nil }
func deny(context.Context, string, runlog.RunMeta) error  { return errors.New("denied") }

func principal(name string, ok bool) func(*http.Request) (string, bool) {
	return func(*http.Request) (string, bool) { return name, ok }
}

func newHandler(r Reader, authz Authorizer, who func(*http.Request) (string, bool)) *Handler {
	h := New(r, authz, who, slog.New(slog.DiscardHandler))
	h.Heartbeat = 20 * time.Millisecond
	h.Poll = 5 * time.Millisecond
	return h
}

// serve runs one request against a live server and returns the body once the
// stream ends.
func serve(t *testing.T, h *Handler, lastEventID string) (*http.Response, string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET /api/stream/runs/{runID}", h)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/stream/runs/run-1", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return resp, string(body)
}

func TestStreamsFromTheBeginningAndStopsAtTerminal(t *testing.T) {
	r := newFakeReader()
	r.append(1, "token", `{"t":"a"}`)
	r.append(2, "token", `{"t":"b"}`)
	r.append(3, runlog.TerminalEventType, `{"status":"succeeded"}`)

	resp, body := serve(t, newHandler(r, allow, principal("alice", true)), "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	for _, want := range []string{"id: 1", "id: 2", "id: 3", "event: done", `data: {"t":"a"}`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n%s", want, body)
		}
	}
}

// Resume replays from the cursor: no gap, no duplication. This is the property
// a rolling deploy exercises on every release, and the one an in-process replay
// buffer fails.
func TestResumeFromCursorSkipsNothingAndRepeatsNothing(t *testing.T) {
	r := newFakeReader()
	for i := int64(1); i <= 5; i++ {
		r.append(i, "token", `{"t":"x"}`)
	}
	r.append(6, runlog.TerminalEventType, `{}`)

	_, body := serve(t, newHandler(r, allow, principal("alice", true)), "3")

	for _, gone := range []string{"id: 1", "id: 2", "id: 3"} {
		if strings.Contains(body, gone+"\n") {
			t.Errorf("resume replayed %s; it should start after the cursor\n%s", gone, body)
		}
	}
	for _, want := range []string{"id: 4", "id: 5", "id: 6"} {
		if !strings.Contains(body, want) {
			t.Errorf("resume skipped %s\n%s", want, body)
		}
	}
}

// A live tail: events appended after the connection opens must arrive.
func TestTailsEventsAppendedAfterConnecting(t *testing.T) {
	r := newFakeReader()
	r.append(1, "token", `{"t":"first"}`)

	go func() {
		time.Sleep(30 * time.Millisecond)
		r.append(2, "token", `{"t":"second"}`)
		time.Sleep(30 * time.Millisecond)
		r.append(3, runlog.TerminalEventType, `{}`)
	}()

	_, body := serve(t, newHandler(r, allow, principal("alice", true)), "")
	for _, want := range []string{`{"t":"first"}`, `{"t":"second"}`, "event: done"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\n%s", want, body)
		}
	}
}

// Heartbeats keep the connection alive and MUST NOT advance the cursor. A
// heartbeat carrying an id would make the next resume skip real output.
func TestHeartbeatsCarryNoID(t *testing.T) {
	r := newFakeReader()
	r.append(1, "token", `{"t":"a"}`)

	go func() {
		time.Sleep(120 * time.Millisecond)
		r.append(2, runlog.TerminalEventType, `{}`)
	}()

	_, body := serve(t, newHandler(r, allow, principal("alice", true)), "")

	if !strings.Contains(body, ": ping") {
		t.Fatalf("no heartbeat in a stream that idled\n%s", body)
	}
	for _, frame := range strings.Split(body, "\n\n") {
		if strings.Contains(frame, ": ping") && strings.Contains(frame, "id:") {
			t.Errorf("heartbeat frame carries an id:\n%q", frame)
		}
	}
}

// Unauthenticated must be a STATUS, not a 200 with an HTML body — the failure
// that made EventSource unusable here.
func TestUnauthenticatedIsAStatusNotAStream(t *testing.T) {
	r := newFakeReader()
	resp, body := serve(t, newHandler(r, allow, principal("", false)), "")

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "event-stream") {
		t.Errorf("Content-Type = %q; an auth failure must not look like a stream", ct)
	}
	if strings.Contains(body, "id: ") {
		t.Errorf("body looks like a stream: %q", body)
	}
}

// Unauthorized gets 404, not 403 — a 403 confirms the run exists.
func TestUnauthorizedGetsNotFound(t *testing.T) {
	r := newFakeReader()
	resp, _ := serve(t, newHandler(r, deny, principal("mallory", true)), "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestUnknownRunIsNotFound(t *testing.T) {
	r := newFakeReader()
	r.meta.ID = "some-other-run"
	resp, _ := serve(t, newHandler(r, allow, principal("alice", true)), "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestInvalidCursorIsRejected(t *testing.T) {
	r := newFakeReader()
	for _, bad := range []string{"abc", "-1", "1.5"} {
		mux := http.NewServeMux()
		mux.Handle("GET /api/stream/runs/{runID}", newHandler(r, allow, principal("alice", true)))
		srv := httptest.NewServer(mux)

		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/stream/runs/run-1", nil)
		req.Header.Set("Last-Event-ID", bad)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		_ = resp.Body.Close()
		srv.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Last-Event-ID %q: status = %d, want 400", bad, resp.StatusCode)
		}
	}
}

// A client that disconnects mid-stream must not wedge the handler.
func TestClientDisconnectEndsTheStream(t *testing.T) {
	r := newFakeReader()
	r.append(1, "token", `{"t":"a"}`)

	mux := http.NewServeMux()
	mux.Handle("GET /api/stream/runs/{runID}", newHandler(r, allow, principal("alice", true)))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream/runs/run-1", nil)

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	buf := make([]byte, 64)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	cancel()
	_ = resp.Body.Close()

	// The handler should return promptly rather than spinning on a dead
	// connection. Appending more must not panic.
	time.Sleep(50 * time.Millisecond)
	r.append(2, runlog.TerminalEventType, `{}`)
}

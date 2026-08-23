# `mindclade_telemetry_spool`

Segmented append-only event persistence with bounded record sizes, disk budgets,
monotonic sequence numbers, replay watermarks, durable acknowledgements, and safe
compaction. Remote delivery remains in the owning telemetry-forwarder service.

Overflow policy is explicit backpressure: once `maximum_total_bytes` is
committed, `append` rejects the newest event with `ResourceExhausted` rather
than growing, and the budget is returned only by acknowledgement plus
compaction. Because the sequence counter is published after the record it
describes is durable, `open` reconciles it against the newest segment: trusting
a counter left behind by a crash would re-issue a live sequence, fail every
replay's monotonicity check, and wedge the spool at its budget permanently.

## Spooling telemetry events

`Envelope.payload` is opaque bytes, and for a while nothing declared what a
spooled telemetry record contained: this crate and `mindclade_telemetry` shared
no type and no dependency edge, and no service appended to the spool at all.
`event_codec` is that declaration and `SpoolSink` is the producer.

`SpoolSink` implements `mindclade_telemetry::Sink`, so any `Logger` or service
adapter already built against that trait gains a durable path by construction.
The adapter lives here rather than in `telemetry` because `telemetry` is
Layer 1 and this crate is Layer 3, and `libs/rust/LAYERS.md` makes production
dependencies downward-only — only the higher crate may name both types.

`append` fsyncs before it returns, so an event `emit` accepted is on disk when
`emit` returns; `Sink::flush` is therefore a no-op rather than an omission.

Degradation is the part worth knowing. A spool at its disk budget rejects with
`ResourceExhausted`, which lasts until a forwarder acknowledges and compacts.
Failing the emitting operation for it would let a full telemetry disk take down
request serving, so `SpoolSink` counts that specific rejection in `dropped()`
and returns `Ok`. Every other fault propagates — a malformed event or a broken
filesystem is not something to absorb. Publish `dropped()` as a counter: a
non-zero value says the forwarder is behind, or is not running.

Nothing is spawned. Draining is the composition root's job and the pieces are
already here: `delivery::deliver_after` reads a bounded batch, hands it to a
`BatchSink`, and acknowledges the highest delivered sequence, after which
`compact` reclaims fully acknowledged segments. Run that from the process's own
supervised task on a bounded cadence, and once more inside the shutdown budget.

The payload is length-delimited via `mindclade_record_io` and every field is
bounded, with every bound re-checked on decode: a segment is a file on a node's
disk that a forwarder reads back after a crash, so the decoder treats it as
untrusted regardless of who wrote it. Timestamps are Unix milliseconds, the
same resolution `Envelope.timestamp_millis` carries, so the envelope and its
payload cannot drift apart about when an event happened.

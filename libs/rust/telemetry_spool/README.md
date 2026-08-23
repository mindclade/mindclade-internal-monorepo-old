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

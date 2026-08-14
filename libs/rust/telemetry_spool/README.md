# `mindclade_telemetry_spool`

Segmented append-only event persistence with bounded record sizes, disk budgets,
monotonic sequence numbers, replay watermarks, durable acknowledgements, and safe
compaction. Remote delivery remains in the owning telemetry-forwarder service.

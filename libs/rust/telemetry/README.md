# `mindclade_telemetry`

Bounded, deterministic, redaction-aware logs/events/traces/metrics contracts.
The implementation is migrated from the uploaded `observability` crate and
retains its bounded attribute semantics.

## Sinks

`Sink` has four implementations. Three of them put a record nowhere a person
can read it — `NoopSink` discards, `MemorySink` is a test double, and
`FanoutSink` only forwards to other sinks — so a process that did not inject
something out-of-tree emitted into a `NoopSink`.

`WriterSink` is the one that lands records. It renders each event as one line
of JSON and writes it to any `io::Write`: `io::stdout()` for a container
collector, a file for a node-local log. Two policies:

| Policy | `emit` does | Use when |
| --- | --- | --- |
| `write_through` | encodes and writes | low-rate paths, and files whose tail must survive a crash |
| `deferred` | encodes into a bounded staging buffer | hot paths; the composition root calls `flush` |

Deferred staging never grows past its budget and never blocks: an event that
does not fit is dropped and counted by `dropped()`. A non-zero drop count means
the flush cadence is too slow for the emit rate, and is the thing to alarm on.
The minimum budget is one maximal record, so a freshly flushed buffer always
admits the next event.

Nothing here is installed and nothing is spawned. There is no global
subscriber, no static registry, and no background thread; the writer is passed
in and draining is a call a composition root makes. `libs/rust/SECURITY.md`
requires that a foundation crate create no ambient async runtime, global thread
pool, or hidden provider client, which is why a batteries-included telemetry
framework was evaluated and declined for this tier.

For durable buffering across a collector outage, use `SpoolSink` in
`mindclade_telemetry_spool`. It lives there, not here: `telemetry` is Layer 1,
the spool is Layer 3, and `libs/rust/LAYERS.md` makes production dependencies
downward-only.

## Record shape

One JSON object per line, members in a fixed order, deterministic for a given
event:

```json
{"time":"2026-03-20T09:46:40.123Z","level":"INFO","msg":"checkpoint.committed","event.id":"evt_0193...","trace.id":"4bf9...","span.id":"00f0...","trace.sampled":true,"step":42,"token":"[REDACTED]"}
```

`time`, `level`, and `msg` are the member names `log/slog`'s JSON handler
writes on the Go side, `trace.id`/`span.id`/`trace.sampled` are exactly the
keys `libs/go/observability.TraceContext.Attributes` produces, and `[REDACTED]`
is byte-identical to `libs/go/faults.RedactedValue`. Attributes flatten as
siblings, the way `slog` flattens its attrs, so one collector pipeline parses
both tiers without a per-language schema. `Attributes::RESERVED_KEYS` keeps an
attribute from colliding with an envelope member; an ill-formed trace context
is omitted rather than exported, matching what the Go tier does with one.

Timestamps carry fixed millisecond precision rather than `slog`'s
trailing-zero-trimming RFC3339Nano, so two renderings of one instant are
byte-identical and the resolution matches what the spool envelope stores.

Encoding only. A decoder would be a parser over whatever a log file happens to
contain, which is an untrusted-input surface this crate has no reason to own;
records that must survive a round trip inside the fleet go through the spool's
length-delimited binary envelope instead.

## Metrics

`CounterRegistry` is **monotonic counters only** — no gauges, no histograms,
no label dimensions — and `prometheus_text()` renders it as Prometheus text
exposition format 0.0.4.

That scope is a deliberate stopping point rather than an oversight. Three
options were on the table:

1. **Generalize the registry** into the Go tier's measurement model (kinds,
   units, labels, `Measurement`). This is the largest change and the one with
   the least immediate return: nothing in the Rust tree records a gauge or a
   histogram today, so it would ship an API with no callers and a second,
   partial copy of `libs/go/observability` to keep aligned by hand.
2. **Render the registry that already exists.** Four of the five Rust services
   held counters they could not export, because the only Prometheus-shaped
   output in the tree was fifteen hand-written lines inside
   `services/ai_gateway_proxy`. Lifting those into this crate gives every
   service the same body from the same code.
3. **Adopt OTLP.** The right long-run answer, and the one that actually
   converges Go and Rust rather than approximating it — but it is a dependency
   decision (`opentelemetry`, `tonic` exporters, a collector endpoint contract)
   that belongs in an ADR alongside the Go tier's existing OTel stack, not in a
   change whose job was to make telemetry reach somewhere at all.

This crate does (2). (1) is not blocked by anything here — the registry can
grow kinds when a caller needs one — and (3) is recorded as follow-up work.

Counter names follow `libs/go/observability.validMetricName` exactly:
lowercase ASCII, `[a-z0-9._]`, a letter first, no trailing separator, no
adjacent separators, at most 128 bytes. Sharing the rule is what lets one fleet
metric be named from either tier. Rendering prefixes `mindclade_`, folds `.` to
`_`, and suffixes `_total`, so `ai_gateway.accepted` becomes
`mindclade_ai_gateway_accepted_total` — the spelling the gateway already
served.

Three admission rules keep the exposition well formed and bounded:

- A name outside the charset is refused, so a name carrying a newline cannot
  forge an extra sample line in the scrape body.
- A name whose rendered series collides with an existing one is refused. The
  `.`-to-`_` fold is not injective, and two counters interleaving samples into
  one series is invisible at the scrape.
- Cardinality is capped at `MAX_COUNTERS`. A registry backs a `/metrics` body,
  so an unbounded name space is an unbounded response.

Use `register()` at construction to publish a service's full counter set at
zero. An absent series is indistinguishable from missing instrumentation, and
`rate()` over a series that first appears mid-window is wrong at its first
sample.

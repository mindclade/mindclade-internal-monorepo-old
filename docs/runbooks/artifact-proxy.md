# Runbook: artifact proxy

Serves the `runtime.artifact_proxy` component (`services/artifact_proxy`).

## Scope note, stated plainly

The binary composes and stays resident. `services/artifact_proxy/src/bootstrap.rs` reads its
configuration from the environment, constructs the object store and CAS, registers both components
with the `servicekit` lifecycle, and serves a bounded operational plane. Probes have something to
target:

| Endpoint | Meaning |
| --- | --- |
| `GET /healthz` | Liveness. 503 once the accounting latch trips — see hazard 1. |
| `GET /readyz` | Readiness. Re-probes the object store on every request. |
| `GET /metrics` | The six transfer counters plus the health gauges. |

What it does **not** serve is artifact bytes. The tenant-scoped byte plane has no wire contract in
`protocols/` — the `ArtifactService` there is the control plane's catalog, which this service does
not own — so reads and writes still arrive only through in-process calls to
`mindclade_artifact_proxy`. Readiness is scoped to what the operational plane actually answers for;
a ready instance is not by itself evidence that any caller can fetch bytes from it.

The process fails closed. A missing or unparsable environment variable, a store root that is not an
absolute path, or an object store that does not answer within five seconds at startup all exit
**78** (`EX_CONFIG`) with the fault message on stderr, before the listener binds.

## Trigger

Reads or writes are rejected, the service stops accepting work, a garbage-collected object is still
being served, or transfer latency is disproportionate to the bytes requested.

## Hazard 1 — the accounting latch is one-way and restart-only

`mark_accounting_corrupt` clears **both** `accounting_healthy` and `accepting`
(`services/artifact_proxy/src/health.rs:98-101`). It fires on counter overflow or underflow
(`:47`, `:75`). No method anywhere restores either flag.

Consequences an operator must know before triaging:

- The instance will never accept work again. There is no drain-and-resume, no reset endpoint, and
  no self-heal. **Recovery is process restart or instance replacement, full stop.**
- The admission check tests both flags together
  (`services/artifact_proxy/src/health.rs:36-38`), so from the outside a corrupted instance looks
  identical to one that is deliberately draining. Distinguish them by reading `accounting_healthy`
  in the health snapshot (`:95`) — not by inferring from `accepting`.
- Counter overflow or underflow means the byte accounting disagreed with itself. Restarting clears
  the latch but not the cause. Capture the snapshot before restarting, or the evidence is gone.

Do not add a setter to clear this latch as an operational convenience. It is one-way on purpose:
accounting that has already proven inconsistent must not keep authorizing transfers.

## Hazard 2 — a garbage-collected object keeps being served

The cache verifies the digest on insert (`services/artifact_proxy/src/cache.rs:63-66`), so it never
serves bytes that do not match their digest. But its entire public surface is `new`, `get`,
`insert`, `bytes`, and `entries`
(`services/artifact_proxy/src/cache.rs:35,51,63,126,133`) — **there is no invalidation API**.

So when an object is garbage collected in durable storage, any proxy instance holding it in cache
continues serving it until LRU pressure happens to evict it. There is no way to force that eviction
short of restarting the instance.

If a GC or retention action must actually take effect on the read path, restart the proxy
instances. Treat "the object is deleted" and "the object is no longer served" as two separate
facts, and verify the second one.

## Hazard 3 — range reads cost the whole object

`read_range` authorizes the range, then calls the full `read` and slices the result in memory
(`services/artifact_proxy/src/transfer.rs:81-89`). A one-byte range of a large object transfers,
and buffers, the entire object.

When investigating latency or memory pressure, do not assume cost tracks the requested range. It
tracks object size. The grant's range checks (`:84`) bound what a caller may *see*, not what the
service does to produce it.

`ProviderArtifacts::read_digest_range` (`src/provider.rs`) now behaves the same way. It previously
called the provider's `get_range` directly, which takes no digest and therefore returned unverified
bytes; it now fetches whole, verifies, and slices. That closed a correctness hole at the cost of
making this hazard uniform across both read paths rather than only the `transfer.rs` one.

## Bounds already enforced

| Bound | Where | Note |
| --- | --- | --- |
| Grant read budget (`maximum_read_bytes`) | `src/grants.rs:89-94` | The **only** ceiling on reads |
| Service write ceiling (`maximum_write_bytes`) | `src/server.rs:113-118` | Checked before work begins |
| Cache byte and entry capacity | `src/cache.rs:35` | Bounded LRU |
| Digest verified on cache insert | `src/cache.rs:64-66` | Rejects mismatched bytes |
| Range validated against object size | `src/transfer.rs:83-84` | Plus `maximum_range_bytes` |

Note the asymmetry deliberately: `write` has a service-level size pre-check
(`src/server.rs:113`); `read` (`:72`) has none. A caller with a generous grant can request an
arbitrarily large read and the service will not independently refuse it. When sizing an instance,
the grant budget is the operative limit.

## Triage

1. Read the health snapshot — `GET /healthz` renders it. If `accounting_healthy` is false, go to
   hazard 1; nothing else will recover the instance. The latch is published as `Unhealthy`, so
   `/healthz` answers 503 and an orchestrator with a liveness probe restarts the instance without
   being asked. That is the intended recovery: capture the body before the restart lands, because
   it is the only record of the latch firing.
2. Check whether the fault is a grant rejection (`PermissionDenied` — range reads not allowed,
   `src/transfer.rs:74-79`) or a budget rejection (`ResourceExhausted` — grant or service ceiling).
   These are caller-scoped and do not indicate service degradation.
3. For stale content, assume cache retention (hazard 2) before suspecting durable storage.
4. For latency, check object size, not range size (hazard 3).

## Recovery

- Corrupted accounting: capture the snapshot, then restart or replace the instance.
- Stale served object after GC: restart proxy instances to clear cache.
- Digest mismatch on insert (`data_loss`, `src/cache.rs:65`): follow `artifact-corruption.md`. Do
  not retry blindly; the bytes did not match their digest.
- Budget rejections: adjust the grant through its issuing authority, never by relaxing the ceiling
  in service configuration to unblock one caller.

## Exit criteria

Instances report `accounting_healthy`, GC'd objects are confirmed no longer served, digests verify,
and no bound or latch was relaxed to restore throughput.

## Known limitations recorded here deliberately

- No byte wire protocol. The process listens only for the probes and metrics above; artifacts
  cannot be fetched from it over the network because `protocols/` defines no contract for the byte
  plane.
- Six counters exist — `read_requests`, `read_bytes`, `write_requests`, `write_bytes`,
  `cache_hits`, `rejected` (`src/telemetry.rs:28-33`). They are now exported at `GET /metrics`
  alongside `artifact_proxy_active_transfers`, `artifact_proxy_accepting`, and
  `artifact_proxy_accounting_healthy`. The exposition is plain `name value` text, not a registered
  metrics format with types or labels.
- The proxy does not verify artifact **manifests**. `src/verification.rs` appeared to provide that
  and was dead code duplicating `object_store::verification`; it was removed rather than left to
  imply a capability that does not exist. Digests are verified — on CAS read and on cache insert.
- No logs and no traces. There is no request-level diagnostic trail.

# Runbook: artifact proxy

Serves the `runtime.artifact_proxy` component (`services/artifact_proxy`).

## Scope note, stated plainly

The shipped binary is a composition seam. It prints one line saying provider and network composition
must be supplied by deployment wiring, then exits
(`services/artifact_proxy/src/main.rs:7-12`). Nothing listens. A liveness probe has nothing to
target, and no operator will find this process running and serving traffic today. What follows
documents the reusable core in `mindclade_artifact_proxy`, so that it is correct when deployment
wiring exists.

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

1. Read the health snapshot. If `accounting_healthy` is false, go to hazard 1 — nothing else will
   recover the instance.
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

- Binary is a stub; no process serves traffic today.
- Six counters exist — `read_requests`, `read_bytes`, `write_requests`, `write_bytes`,
  `cache_hits`, `rejected` (`src/telemetry.rs:16-27`) — in an in-process registry with **no
  exporter**. They are unreachable from outside the process until deployment wiring exports them.
- No logs and no traces. There is no request-level diagnostic trail.

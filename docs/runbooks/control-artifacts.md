# Runbook: control artifacts

Serves the `control/artifacts` package — artifact identity, locations, access grants, retention, and
garbage-collection planning.

## Scope note, stated plainly

`control/artifacts` has no process of its own and no production caller. The only implementation of
its `Catalog` seam (`control/artifacts/catalog.go:56-61`) is the in-memory `MemoryCatalog`
(`control/artifacts/repository.go:14-20`), constructed only by the package's own tests
(`control/artifacts/artifacts_test.go:16,34`). `BuildGCPlan` has no caller outside the package
either. This runbook documents the real failure surface so it is correct when a durable catalog and
a caller exist; it is not evidence that artifact control is running in production.

## Trigger

Artifact registration is rejected, a digest cannot be re-registered after a failed attempt, a
garbage-collection plan produces unexpected candidates or none at all, or a GC receipt fails
validation.

## Failure modes and what each one means

### `artifact_identity_conflict` — the digest is already bound to different immutable metadata

A digest binds to immutable metadata permanently (`control/artifacts/repository.go:31-33`). The
comment on `Register` records why this matters: a registration that failed halfway used to leave the
binding behind and poison the digest, so a corrected retry with different metadata then failed
against state the caller never asked to keep
(`control/artifacts/service.go:11-23`). `Register` now performs every rejection it can make
**before** the first write (`:26-38`), so a poisoned digest should no longer be reachable through
this path.

If this fault is seen anyway, do not "fix" it by overwriting the binding. Determine which metadata
is correct. Identical content is idempotent; different content means two producers disagree about
what a digest denotes, which is an integrity question, not a retry question.

### Registered identity with no location

`Register` writes the ref, then each location, as separate catalog calls
(`control/artifacts/service.go:38-45`). The catalog seam has no transaction, so a crash between them
leaves a registered identity with no location. This is documented and deliberate
(`control/artifacts/service.go:17-23`): recovery is simply to **replay `Register` with the same
arguments**, because `Put` and `PutLocation` are both idempotent for identical content.

Do not manufacture a location record to paper over this. Replay the registration.

### `artifact_location_identity_mismatch` / `artifact_location_unknown_identity`

A location must match the identity it is registered against
(`control/artifacts/service.go:35-37`) and the identity must already exist
(`control/artifacts/repository.go:52-54`). Both are caller defects.

### A GC plan produced no candidates

This is usually correct behaviour, not a fault. `GCPolicy.Evaluate` is fail-closed: it returns a
candidate only when **every** blocker clears (`control/artifacts/gc.go:99-140`). The blockers are
enumerated as `GCBlockReason` (`control/artifacts/gc.go:17-24`) and each maps to a specific check:

| Reason | Meaning | Source |
| --- | --- | --- |
| `reachable` | Any durable, release-evidence, or audit reference exists | `gc.go:27-31,101-103` |
| `active_lease` | A lease has not expired | `gc.go:33-36,104-109` |
| `administrative_pin` | A pin is present and unexpired (a nil `ExpiresAt` never expires) | `gc.go:38-46,110-115` |
| `retention_hold` | A hold is present and unexpired (a nil `Until` never expires) | `gc.go:48-56,116-121` |
| `retention_window` | `CreatedAt` is zero, or the minimum age for the kind has not elapsed | `gc.go:79-90,122-125` |
| `location_invalid` | Object path fails validation, or observed version is empty or over 512 bytes | `gc.go:92-97,126-128` |

Read the returned `BlockedBy` list before concluding anything. Note that a pin or hold with a nil
expiry blocks forever by design; if collection is genuinely required, the pin or hold is what must
be lifted, by its owner. Never relax `GCPolicy` to unblock a single artifact.

### GC deletion targeting

The byte plane must delete only the exact `ObjectPath` and `ObservedObjectVersion` captured when the
plan was built, and must never reconstruct a path from an artifact URI
(`control/artifacts/gc.go:63-66`). `validGCObjectPath` rejects empty paths, paths over 1024 bytes,
untrimmed values, leading or trailing `/`, `//`, backslashes, NUL bytes, `.`, `..`, and any `../`
traversal (`control/artifacts/gc.go:92-97`).

If a deletion is proposed against a path that does not appear verbatim in the plan, stop. That is a
traversal or a stale-plan condition, and it deletes the wrong object.

### GC receipt validation failure

`ValidateGCReceipt` (`control/artifacts/gc.go:185`) checks the receipt against the plan it claims to
satisfy. A failure means the byte plane did something other than what was planned. Preserve both the
plan and the receipt, and follow `artifact-corruption.md` if any object was already removed.

## Recovery

- Half-completed registration: replay `Register` with identical arguments.
- Blocked GC: identify the blocker from `BlockedBy` and route to the owner of the lease, pin, or
  hold. Do not edit policy to unblock.
- Stale plan: rebuild the plan. Object versions observed at plan time are the safety mechanism;
  a plan whose observed versions no longer match must not be executed.

## Exit criteria

Artifact identities and locations are consistent, GC decisions are explained by their recorded
blockers, no object was deleted outside a plan, and no bound or policy check was relaxed.

## Known limitations recorded here deliberately

- No metrics, logs, or traces are emitted by `control/artifacts`. Every signal above is a fault
  reason or a returned decision observed by the caller.
- There is no durable catalog. `MemoryCatalog` loses all state on restart and is not a production
  implementation.

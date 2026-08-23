# CI Bazel validation runbook

## Gazelle ownership

`//:gazelle` owns native Go BUILD metadata and `//:gazelle_check` verifies the checkout without
modifying it. The generator is intentionally limited to Gazelle's Go language extension; Python,
Proto, and visibility generation remain outside its authority. The affected selector never
injects this target because comparison bases may predate its creation. Invoke it explicitly when
qualifying Gazelle; protected full-graph runs include every target present at their revision.

Run the generator only when reconciling reviewed Go source changes:

```bash
tools/dev/nixw develop .#ci-bazel --command tools/dev/bazelw run //:gazelle
tools/dev/nixw develop .#ci-bazel --command tools/dev/bazelw test //:gazelle_check --config=ci
```

Full-graph sharding remains deferred until the shared Bazel remote cache is connected and
qualified. Sharding hosted runners while the bounded GitHub-transported disk cache is the only
persistent cache would split warm state across workers and can increase cold compilation cost.
Do not add matrix shards or claim a latency improvement before retained connected cache evidence
supports that transition.

## Persistent action cache

The immediate persistent action cache is Bazel's local `--disk_cache`, transported between
ephemeral hosted runners by GitHub Actions cache. It is not a remote REAPI cache, is not backed by
Cloud Storage, and does not remove the activation block on the future authenticated cache gateway.

- The cache key binds runner OS/architecture, `.bazelversion`, the Bzlmod module and lock,
  `REPO.bazel`, `flake.lock`, the committed Nix toolchain manifest, and a trusted commit SHA.
- Pull requests restore against `pull_request.base.sha`; merge groups restore against
  `merge_group.base_sha`. Both are read-only and never upload local action results.
- A failed restore is non-authoritative: CI discards any partial cache tree and continues cold.
  Quiescence, measurement, and save steps are also non-blocking; their outcomes remain visible in
  evidence while the analysis/test verdict stays authoritative.
- Protected `push` runs on `main` and main-only scheduled/manual nightly runs may save. The governed
  pull-request and merge-group paths do not call the save action. GitHub isolates any cache created
  under a temporary pull-request or merge-queue ref so it cannot become a trusted main-branch input;
  those refs may still restore a cache published from the default branch.
- `user.bazelrc` is generated after restore and before the first Bazel invocation. It points only at
  `$RUNNER_TEMP`, makes cache uploads synchronous, enables digest verification and compression,
  caps Bazel 9 garbage collection at 1 GiB with a one-second idle delay, and contains no
  credentials.
- Before measurement or persistence, CI waits for the idle GC boundary and shuts down the Bazel
  server. Shutdown cancels or waits for background GC and upload work, leaving a stable cache tree;
  an over-limit or failed-quiescence run never calls the save action.
- Treat restored cache contents as pull-request-readable. The workflow clears setup-bazel's
  download token before Bazel starts, and no credential, secret, or sensitive generated output may
  enter the cache path.
- `cache-metrics.json` and the job summary distinguish exact/prefix restores, an unverified
  not-restored result, and a restore action error. They record the save action's step outcome only
  as an unverified attempt, plus role, trusted revision, and final file bytes when quiescence and
  measurement succeeded. They never claim a save, empty restore, or failed measurement is verified,
  or record action contents or tokens.

Treat a cache hit only as a performance optimization. Bazel still validates action digests and the
normal analysis/test verdict remains authoritative. If corruption is suspected, rerun from a new
cache namespace or remove the affected cache through the reviewed GitHub cache operation; do not
weaken tests or reuse a cache across a changed toolchain fingerprint. Rollback removes the restore,
save, and generated `user.bazelrc` steps; the next run must succeed as a cold local build.

## GCS remote-cache activation

The production successor is the loopback gateway at
`//tools/build/bazel/cache_gateway/cmd:cache_gateway`, backed by the separately hardened common-CI
Cloud Storage bucket. It implements Bazel's HTTP AC/CAS paths while keeping cloud authentication
out of Bazel: the auth action's external-account file is validated, moved from the checkout into a
private runtime directory, and inherited only by the gateway process. The gateway publishes with
generation-zero create-only preconditions, verifies CAS body digests, pins object generations on
reads, and treats identical duplicate uploads as idempotent. A different payload at an existing
key is a redacted `immutable_collision`, never an overwrite.

Activation currently remains blocked in `ci/bazel_cache/activation.json`. The Bazel jobs also omit
`id-token: write`; therefore the server-side repository variable cannot activate WIF by itself.
The reviewed activation change must do all of the following together:

1. Confirm immutable module release `v0.4.0` and exact applied bucket
   `${CI_PROJECT_ID}-bazel-cache`.
2. Record a restricted, retention-governed evidence object generation, its SHA-256, reviewer, and
   UTC review timestamp under
   `gs://mc-production-qualification-evidence/bazel-cache/` in the activation contract.
3. Prove bucket retention, CMEK, versioning, soft-delete recovery, public-access prevention,
   access logging, denied reader writes, denied writer deletes, cold rebuild, warm hit, CAS
   integrity, identical duplicate, immutable collision, corrupt-download rejection, cache loss,
   bounded concurrent-staging load without temp-disk exhaustion, and every positive and negative
   WIF route.
4. Change the source record and governed repository variable to `qualified-v1` and add job-scoped
   `id-token: write` to only the Bazel jobs. Workflow-level OIDC permission remains forbidden.
5. Observe pull requests in reader mode and protected main, merge group, and scheduled nightly in
   writer mode. Manual nightly remains disabled and continues using the disk cache.

When qualified, the disk-cache steps are mutually exclusive with the remote path. The gateway
listens only on `127.0.0.1`, permits at most one-GiB objects, spools and verifies a complete GET
before returning success, and permits two combined GET/PUT staging files. It emits counters without
keys or digests and is stopped before its credentials are removed. `cache-metrics.json` records the
gateway binary SHA-256, role, hits/misses, create/idempotent/rejected/collision counts, bytes, and
staging maximum/active/peak/wait/cancellation values; `cache-gateway.log` contains only stable error
codes and methods. BEP/profile evidence remains the authority for Bazel action-cache hits and
critical-path changes.

Rollback first returns the governed repository variable to `blocked`, then removes job OIDC in a
reviewed source change and restores the source record to `blocked`. The disk cache becomes active
on the next run. Do not delete cache objects as part of client rollback; lifecycle policy and a
separate reviewed incident operation own deletion.

## build-cache-immutable-collision

A write presented a different payload at an existing content-addressed key and the gateway rejected
it with a redacted `immutable_collision`. Identical duplicate uploads are idempotent by design, so a
collision is never a benign retry: it means a digest has stopped identifying exactly one payload.
Until the cause is understood the cache is no longer content-addressed, and every hit served from it
is a claim nobody can check.

Treat it as a supply-chain event rather than a cache fault. Preserve `cache-metrics.json` and
`cache-gateway.log` from the affected run — the log carries stable error codes and methods only, so
it is safe to attach — and identify the writing role before anything else. Do not delete the object
to "clear" the collision: lifecycle policy and a separate reviewed incident operation own deletion,
and removing the evidence removes the only record of which payload arrived first.

## build-cache-reader-write-denied

A reader-role identity attempted a write and was denied. The deny held, which is why this is a
ticket and not a page — nothing is broken, and the fail-closed path did its job.

The benign explanation is a misconfigured client: `--remote_upload_local_results` left on in a lane
that is supposed to be read-only, or a `user.bazelrc` pointed at the gateway on a machine that has
no business writing it. Confirm that first, because it is the common case and the fix is a one-line
configuration change.

The explanation that matters is the presubmit lane. Untrusted pull-request code attempting cache
writes is exactly the boundary the ARC runner-group split exists to hold; see
[`arc-runners.md`](arc-runners.md). If the attempts originate there, treat it as a stop condition on
presubmit activation rather than as a client bug, and do not widen the write grant to make the
signal stop.

## build-cache-gateway-request-error-ratio

Gateway requests are failing for reasons other than a cache miss. The threshold is proposed, not
approved, and needs a real load baseline behind it before anyone commits to it.

A miss is normal and cheap; an error is neither. Bazel cannot distinguish a broken cache from an
absent action result, so a gateway erroring at a sustained rate converts a performance optimization
into build failures and latency that look like a compiler problem. Read the stable error codes in
`cache-gateway.log` first: staging-wait cancellations point at the bounded spool and concurrency,
digest-verification failures point at object integrity, and authentication failures point at the
external-account file rather than at the cache.

The rollback is the one already documented above — return the governed repository variable to
`blocked`, then remove job OIDC and restore the source record. The disk cache becomes active on the
next run, and a cold build is the correct outcome. Never weaken digest verification or raise the
one-GiB object limit to make errors go away.

## Failure triage

1. Read `selection.json` first. Confirm event, base/head, effective mode,
   fallback reason, seeds, and selected target counts.
2. For Git failures, verify the checkout is unshallow and the recorded base is
   an ancestor of `HEAD`. Do not substitute an arbitrary branch name.
3. For query/loading failures, use the first external-repository or BUILD error
   from the log. Retry once only for a confirmed transient registry/network
   event; repeated failure remains red.
4. For analysis/test failures, reproduce the exact target-pattern file under
   `.#ci-bazel`, then expand to full mode if graph ambiguity remains.
5. For latency misses, inspect the trace profile and BEP summary for loading,
   repository fetch, analysis, execution, cache, and critical-path growth. Compare
   `cache-metrics.json` first so cold, exact-hit, and prefix-hit runs are not mixed.
6. During selector incidents, change the workflow invocation to `--mode full`
   without renaming or disabling `bazel / verdict`. Preserve artifacts and the
   failing revision for follow-up qualification.
7. Governance rollback is a separate reviewed `github-config` change. Return a
   required-check ruleset to evaluate before removing or renaming an emitted
   context.

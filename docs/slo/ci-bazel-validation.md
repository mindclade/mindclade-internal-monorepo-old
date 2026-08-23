# CI Bazel validation SLO

## Objective

After a 28-day burn-in, 95% of pull-request runs whose effective selection mode
is `affected` must complete the `bazel / verdict` job within 30 minutes. The
objective remains unqualified until that retained live window is complete.
Global-fallback, merge-group, protected-main, and nightly full runs are excluded
from this latency percentile.

## Correctness

Selection, Git history, Bazel loading/query, configured analysis, tests, and
evidence normalization fail closed. Correctness is not traded for latency: a
query error or unknown structural change never becomes an empty target set.

The GitHub Actions-backed persistent action cache is non-authoritative. The
governed pull-request and merge-group paths restore but do not save; GitHub's
temporary-ref cache scope cannot publish an entry consumed by protected main.
Cache loss, eviction, restore failure, or a changed toolchain fingerprint must
produce a cold local build, never a skipped analysis/test verdict. Cache hits
are not provenance or release evidence. The authenticated remote-cache gateway
remains activation blocked and is outside this SLO. While blocked, the Bazel jobs have no OIDC
permission, so repository-variable drift cannot silently make the gateway load-bearing.

## Measurement

`run-metrics.json` records event, effective mode/reason, job elapsed seconds,
target counts, and the 1,800-second objective. Thirty-five-day workflow
retention covers the burn-in window. Cancelled runs, GitHub platform outages,
and full-mode runs are reported separately rather than removed from raw
evidence.

`cache-metrics.json` records the persistent-cache role, trusted revision,
exact/prefix/not-restored/error restore state, unverified save-attempt step outcome, and
bytes retained under the 1 GiB limit after Bazel shutdown. The latency series
must segment cold not-restored runs, prefix restores, and exact restores. A cache
improvement may reduce the measured percentile but may not change the target
selection, full-graph merge-group/nightly contract, or 75/90-minute correctness
ceilings.

After connected activation, the same file changes to the GCS transport schema and records the
gateway binary SHA-256, reader/writer role, remote hits and misses, immutable creates, idempotent
duplicates, rejected writes and collisions, request errors, transferred bytes, configured staging
concurrency, staging peak, waits, and canceled waits. Segment the latency series by transport and
role. The BEP summary remains the source for action-cache hit rate and execution critical path;
gateway counters are an independent backend cross-check, not a build verdict.

## Promotion gate

The components remain `implemented` until the connected canary matrix passes,
the exact required context is observed on pull requests and merge groups, and
the 28-day p95 objective is calculated from retained evidence. Promotion also
requires a green cold-cache control run, bounded-cache size evidence, and no
trusted save originating outside protected main or main-only nightly.
Remote-cache promotion additionally requires the full activation matrix in
`ci/bazel_cache/activation.json`, exact retained evidence generation and SHA-256, and intentional
negative evidence for feature branches, tags, manual dispatch, altered workflows, wrong
repository IDs, wrong audience, and cross-role impersonation. A connected load run must also prove
the two-slot staging bound prevents temp-disk exhaustion without violating the latency objective.

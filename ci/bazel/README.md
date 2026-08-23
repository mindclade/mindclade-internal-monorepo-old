# Bazel graph ownership and sharding

## Gazelle

`//:gazelle` owns native Go BUILD metadata. `//:gazelle_check` runs the same pinned Go-only
generator in diff mode and fails if the checkout is stale. Run `//:gazelle` only when reconciling
reviewed Go source changes; CI uses the non-mutating check.

The repository does not currently contain a reviewed, locked Python Gazelle extension. Python
BUILD metadata therefore remains manually owned. Do not add implicit Python generation or invent
an unreviewed source archive pin. A future extension must first be pinned in `MODULE.bazel`, locked
for every supported platform, included in downloader/mirror qualification, and proven against the
existing Python graph before its output becomes authoritative.

## Complete graph shards

When the reviewed remote cache is connected, protected `merge_group`, protected `main`, and
scheduled nightly qualification partition the complete non-manual repository rule and test label
graph into four disjoint shards. The sharder enumerates labels with `bazel query`; every worker
then executes its explicit labels under `--config=ci`, where Bazel applies configured analysis and
`target_compatible_with` filtering with the same compatibility semantics as `//...`. Until the
cache is connected, those events retain one complete unsharded `//...` fallback instead of
multiplying cold hosted-runner builds. Analysis rules are placed by stable label hash with balanced
target counts. Tests use deterministic longest-processing-time placement over
retained medians, with a conservative 5,000-millisecond default for tests lacking five clean
observations. That default rounds above the 4,823-millisecond maximum median among audited
all-pass omitted tests.

The query uses Bazel's established `attr("tags", "manual", ...)` filter. The reviewed tree has no
tag containing `manual` other than the exact `manual` tag, so this currently matches the exclusion
semantics of the existing `//...` lanes without relying on an incompatible serialized-list regex.

`full_graph_shards.toml` pins the workflow run, Bazel job, head commit, artifact metadata, and
GitHub-reported SHA-256 for every retained cold-run sample. Timing evidence is not qualification
evidence: only one of the five complete Bazel attempts passed; four concluded failure. The
contract records each failed test and retains weights only for tests observed passing in all five
attempts. A failed `test_reference_affine` observation is deliberately excluded and receives the
conservative 5,000-millisecond default weight until a clean replacement sample is reviewed.

Across those five attempts, the median pre-artifact Bazel qualification path was 4,220.338 seconds
and the median test command was 3,002.008 seconds. The test-command BEP median critical path was
261.175 seconds, with 9,421 median test-command action-cache misses. The planner rejects stale or
failed observed labels, records contract
and queried repository-graph digests in every selection, and proves that the union of all shards
is the entire analysis and test universe without overlap. The reviewed runtime-base tree contains
1,051 non-manual analysis rules and 428 non-manual tests; its four analysis partitions contain 263,
263, 263, and 262 targets. Estimated test duration differs by 4,952 milliseconds across shards.
These figures must be regenerated whenever retained weights or the queried repository graph
change.

Every worker derives a separate `worker-selection.json` from the completed local selection. That
artifact contains only the event, exact head, topology, shard index, and SHA-256 identities for the
contract, complete graphs, and canonical partition manifest; target labels, changed paths, queries,
commands, and timing data are omitted. The stable verdict downloads only the current run-attempt's
redacted artifacts and fails unless their identities agree and their selected indexes cover every
expected shard exactly once. Single-worker pull-request and cold-cache fallback routes must instead
publish the expected sentinel worker with no partition identity.

Pull requests remain one unsharded presubmit worker and obey the reviewed selector activation
contract. While graph-native affected selection is disabled, that worker still executes the full
graph. The merge queue remains the complete correctness gate; nightly provides a second full-graph
observation rather than replacing that gate.

## Output roots, and why their total is not bounded

### The measurement

PR #138 added `ci/presubmit/disk_preflight.py` after a full disk took down thirteen concurrent
agents, and reported that the Bazel output user root held **139 GiB across ~30 worktrees**. That
number was correct and incomplete: it is one root. A direct measurement of the same host on
2026-08-23 (macOS, Bazel 9.1.1, `du -sk` per direct child of each root) found two:

| Output user root | Bases | Live | Orphaned | `cache/` + `install/` | Root total |
| --- | --- | --- | --- | --- | --- |
| `~/Library/Caches/bazel/_bazel_$USER` | 24 | 24 (7.5 GiB) | 0 | 4.2 GiB | **11.6 GiB** |
| `/private/var/tmp/_bazel_$USER` | 29 | 4 (34.3 GiB) | 24 (**97.8 GiB**) | 3.9 GiB | **139.2 GiB** |
| | **53** | 28 (41.8 GiB) | **24 (97.8 GiB)** | 8.1 GiB | **150.8 GiB** |

Two facts in that table matter more than the total.

**The two roots are not a misconfiguration.** Bazel's default output user root on macOS moved
from `/var/tmp/_bazel_$USER` to `~/Library/Caches/bazel/_bazel_$USER`. This repository sets no
`--output_user_root` anywhere, and `XDG_CACHE_HOME` is unset, so a host that has run more than
one Bazel release simply has both. Every base in the `Library/Caches` root was created the day
of the measurement; every base in `/private/var/tmp` was three weeks old or older. The preflight
knew only about `/private/var/tmp`, so it was reporting a root Bazel had stopped writing to.

**Two thirds of the total is unreachable garbage.** Each output base carries a
`DO_NOT_BUILD_HERE` file naming the workspace that owns it. For 24 of the 29 bases in the older
root that workspace no longer exists — deleted scratch clones under `/private/tmp/mindclade-*`,
retired `PoiesisLabs/mindclade-wt-*` checkouts, expired `$TMPDIR` benchmark trees. Nothing will
ever read those 97.8 GiB again. Bazel will not remove them, because Bazel never revisits an
output base it is not currently using.

Reproduce the whole table, exactly and with no time budget:

```sh
tools/dev/nixw develop .#ci --command python3 ci/presubmit/disk_preflight.py --report
```

It probes every root Bazel may have used on the platform, classifies each base `live`, `ORPHAN`,
or `unknown` from its marker file, and prints per-root and reclaimable totals. It takes minutes
on a loaded host; that is why the preflight's abort path does not run it.

### Why nothing bounds the total

Bazel keys the output base on the **workspace path**. Thirty-five agent worktrees plus a drawer
of scratch clones are thirty-five-plus distinct workspaces, so they are thirty-five-plus distinct
output bases, and there is no ceiling, LRU, or expiry on any of them. Deleting a workspace does
not delete its base. This is not a knob that is turned off; Bazel has no such knob.

Note in particular what `ci/common/bazel_disk_cache.py` does and does not bound. It sets
`--experimental_disk_cache_gc_max_size=1G`, and that ceiling applies to the `--disk_cache`
content-addressed store only. It has no effect whatsoever on output bases. The one bounded Bazel
directory on the host is also the smallest.

### Options weighed, and why the total stays unbounded

**A shared `--output_user_root` across worktrees.** Rejected. It consolidates the *location* of
the bases without bounding their number or size — Bazel still keys one base per workspace path
under the shared root — and it buys that non-benefit at a real cost: concurrent builds against
one root contend on the same install and cache locks. Trading build-time serialisation for zero
bytes reclaimed is a bad trade. It would also have to be set somewhere, and the only per-machine
Bazel configuration file this repository has is `user.bazelrc`, which is gitignored and which
governed CI generates and validates as the sole runtime cache authority. A committed startup
flag that redirected the output root would put a second authority in the tree.

**A size-capped `disk_cache` that Bazel can evict from.** Already in place, already bounded at
1 GiB, and orthogonal. It bounds the action cache, not the output bases. Raising it would
increase disk usage, not decrease it.

**A checker that fails the build when the roots exceed a threshold.** Rejected. Sizing 53 bases
is minutes of tree walk, which is not a gate's budget; and the roots are shared machine state
owned by every agent and checkout on the host, so failing one PR's presubmit on a number that
another worktree caused is a gate nobody can act on from inside the change under review.

**What was adopted: name it, and aim the reclamation.** The preflight now enumerates every root
the platform may use, reports the exact structural facts — how many bases exist and which ones
belong to workspaces that no longer exist — and prints that section *ahead* of the sized reclaim
table. The costly, exact measurement lives behind `--report`.

So the honest conclusion is that the total cannot be bounded by configuration: Bazel offers no
mechanism, and the only mechanism that would (one shared base) trades build time for nothing.
What can be bounded is the *garbage*, because an orphaned base is identifiable in one file read
and is unambiguously safe to remove.

### Reclamation procedure

`disk_preflight.py` never deletes anything, and neither should a script that runs unattended. A
sibling worktree's output base is another agent's warm build cache; on this host roughly a dozen
agents have been active at once, and several have lost work to a deleted worktree.

1. List what is reclaimable. The `ORPHAN` rows are bases whose workspace is gone.

   ```sh
   tools/dev/nixw develop .#ci --command python3 ci/presubmit/disk_preflight.py --report
   ```

2. Remove orphaned bases by path, one at a time, from the report's `ORPHAN` rows. Never glob a
   root, and never remove a `live` row: that is a checkout somebody is using, and its base is
   between five minutes and an hour to rebuild.

   ```sh
   rm -rf /private/var/tmp/_bazel_$USER/<basename-from-the-report>
   ```

3. For a workspace that still exists but is finished with, use Bazel rather than `rm`, from
   inside that workspace, so the server shuts down first:

   ```sh
   tools/dev/bazelw clean --expunge
   ```

4. The shared `cache/` under each root is the repository and download cache. It is safe to
   remove and costs refetches rather than rebuilds, which makes it the cheapest multi-gigabyte
   reclaim on the list — but it is shared by every workspace under that root, so removing it
   slows the next build in all of them.

5. Re-run `--report` and confirm the reclaimed total.

Do not add this to a cron job or a presubmit hook. The classification is cheap and safe; the
deletion is neither, precisely because the state is shared.

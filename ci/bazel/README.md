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

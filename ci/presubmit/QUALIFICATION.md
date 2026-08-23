# Presubmit Bazel qualification

## Source evidence

The selector has unit coverage for global and structural fallback, owning
package seeds, deterministic Bazel query construction, transitive results,
manual-tag exclusion, empty selections, invalid Git bases, rename-aware diffs,
query failures, evidence safety, and CLI phase behavior. A repository fixture
exercises the same selector against the real pinned Bazel graph.

The fallback contract is executable rather than prose. Static presubmit loads
the same strict JSON used by the selector, rejects duplicate, unordered,
overlapping, or unsafe entries, inventories tracked and unignored repository
paths, and fails when a new root or `tools/` authority has not been reviewed.
Unit tests seed both classes of drift and require stable error codes.

## Graph-native activation probe (2026-08-22)

`bazel-contrib/target-determinator` `v0.34.0` was reviewed at commit
`d4b6125546979713431e63b5c3e65810fa989446`. The supported-platform release
asset SHA-256 values are recorded in `../common/affected_global_inputs.json`.
The Darwin arm64 asset matched its published digest and reported the expected
version. Through `.#ci-bazel`, a package-scoped comparison against `HEAD^`
completed in 19 seconds and selected the changed serialization library and its
two tests.

The full `//...` probe did not qualify activation. On the available 99%-full
Darwin host it remained in the first `cquery deps(//...)` while free space fell
from 7.5 GiB to 7.0 GiB, so it was interrupted before destabilizing concurrent
work. The interruption left the checkout detached at the before revision until
it was explicitly restored. The tool also could not parse Nix's
`release 9.1.1- (@non-git)` version string and conservatively assumed configured
rule inputs were unavailable. These measured conditions map to the four
machine-readable blockers in the contract. No graph-native selector is active.

## Connected evidence required

Qualification remains pending until pull-request canaries demonstrate leaf,
documentation-only, global, intentional selector-failure, and intentional
test-failure behavior; the exact `bazel / verdict` context is observed on both
pull requests and merge groups; and 28 days of ordinary affected runs meet the
p95 30-minute objective.

Graph-native activation additionally requires all four contract blockers to be
replaced by retained evidence from a trusted x86_64 Linux runner. The transition
must preserve full `//...` execution for merge groups and nightly runs; affected
selection remains a pull-request latency optimization only.

Ruleset evaluation and activation evidence belongs to `github-config`. A local
test pass is not evidence that GitHub is enforcing the context.

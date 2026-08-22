# Presubmit Bazel qualification

## Source evidence

The selector has unit coverage for global and structural fallback, owning
package seeds, deterministic Bazel query construction, transitive results,
manual-tag exclusion, empty selections, invalid Git bases, rename-aware diffs,
query failures, evidence safety, and CLI phase behavior. A repository fixture
exercises the same selector against the real pinned Bazel graph.

## Connected evidence required

Qualification remains pending until pull-request canaries demonstrate leaf,
documentation-only, global, intentional selector-failure, and intentional
test-failure behavior; the exact `bazel / verdict` context is observed on both
pull requests and merge groups; and 28 days of ordinary affected runs meet the
p95 30-minute objective.

Ruleset evaluation and activation evidence belongs to `github-config`. A local
test pass is not evidence that GitHub is enforcing the context.

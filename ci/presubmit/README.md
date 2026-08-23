# CI / Presubmit

- **Status:** Implemented; connected check-context and latency qualification remain pending.
- **Owner:** Release Engineering
- **SLO:** [`../../docs/slo/ci-bazel-validation.md`](../../docs/slo/ci-bazel-validation.md)
- **Runbook:** [`../../docs/runbooks/ci-bazel-validation.md`](../../docs/runbooks/ci-bazel-validation.md)

## Responsibility

`pipeline.py` owns the repository-local presubmit contract. `--static-only`
runs architecture policy without a Bazel toolchain. `--bazel-only` selects and
executes configured Bazel validation inside the pinned `.#ci-bazel` shell.

Protected pull requests use the Bazel post-loading reverse-dependency selector.
Merge groups and protected-main pushes use full mode. Changes to CI, toolchains,
dependency locks, Starlark,
protocols, architecture, component, or maturity policy still force full mode.
The exact inputs and review-boundary inventory are versioned in
`../common/affected_global_inputs.json`; a new repository root or a new
`tools/` authority fails static presubmit until it is classified.

## Interfaces

```text
pipeline.py --static-only
pipeline.py --bazel-only --mode affected --base <commit> --evidence-dir <outside-checkout>
pipeline.py --bazel-only --mode full --evidence-dir <outside-checkout>
```

Affected mode fails when the base is missing, is not an ancestor of `HEAD`, the
diff is malformed, an owning package cannot be established, or Bazel query
fails. A successful diff and query that select no Bazel targets is recorded as
an explicit skip rather than expanded to a false full-graph claim.

## Evidence and limits

The lane writes a versioned selection record, target-pattern files, BEP and
trace-profile evidence, normalized summaries, and run metrics. Evidence paths
must be outside the checkout and must not be symbolic links. Raw Bazel evidence
is produced only after removing the setup action's short-lived launcher token
from the subprocess environment.

The selector still loads the full unconfigured Bazel universe. It reduces
configured analysis, compilation, and test execution; it does not promise that
loading or external-repository resolution is free. GPU, provider,
remote-execution, release, and deployment qualification remain out of scope.

Graph-native comparison is deliberately not active. The checksum-pinned
target-determinator candidate and its blockers are recorded in the global-input
contract. Activation requires a qualified remote cache, an externally pinned
required workflow, a complete x86_64 Linux full-graph comparison, a wrapper that
restores the checkout after interruption, and review of the Bazel 9 version-parsing
fallback. Until those are satisfied, the graph-native implementation remains
inactive; the repository-owned Bazel-query selector continues serving pull requests.

Artifact-plan Phase 5 is therefore **incomplete**. The live pull-request path uses
the conservative Bazel-query implementation; graph-native migration remains blocked.
Workflow YAML is parsed with pinned PyYAML semantics while rejecting aliases, tags,
duplicate keys, and semantic block-scalar drift. The complete ordered Bazel step
sequence is digest-pinned, and a tested event state machine permits affected mode only
for pull requests. The governed step invokes the root-owned Nix installation by absolute
path, uses a read-only Nix-store Git, requires the exact event `GITHUB_SHA` in a canonical
non-worktree checkout, accepts only the exact `$RUNNER_TEMP` cache contract, repeats that
role after `--config=ci`, and parses a canonical non-future integer job-start epoch. These
repository-local
controls do not replace the pinned organization required workflow that must ultimately
enforce the gate from outside a pull request's mutable trust boundary.

## Rollback

Keep merge groups, protected-main pushes, and nightly runs in full mode. A
graph-native migration requires one coordinated reviewed change to the blocker
contract, semantic workflow contract, and orchestration tests after connected
evidence exists. Changing only the workflow argument fails closed.
Retain the exact `bazel / verdict` job name; do not disable the job, weaken failure
behavior, or rename a required context during incident recovery.

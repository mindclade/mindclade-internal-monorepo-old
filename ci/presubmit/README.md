# CI / Presubmit

- **Status:** Implemented; connected check-context and latency qualification remain pending.
- **Owner:** Release Engineering
- **SLO:** [`../../docs/slo/ci-bazel-validation.md`](../../docs/slo/ci-bazel-validation.md)
- **Runbook:** [`../../docs/runbooks/ci-bazel-validation.md`](../../docs/runbooks/ci-bazel-validation.md)

## Responsibility

`pipeline.py` owns the repository-local presubmit contract. `--static-only`
runs architecture policy without a Bazel toolchain. `--bazel-only` selects and
executes configured Bazel validation inside the pinned `.#ci-bazel` shell.

Protected pull requests, merge groups, and protected-main pushes currently use
full mode. The Bazel post-loading reverse-dependency selector remains available
only through explicit local qualification while its activation blockers remain.
After future activation, changes to CI, toolchains, dependency locks, Starlark,
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
fallback. Until those are satisfied, every protected event remains full graph.

Artifact-plan Phase 5 is therefore **incomplete**. The live pull-request path is full
graph; the conservative Bazel-query implementation is exercised only by local and
fixture qualification. Workflow YAML is parsed structurally, the complete ordered
Bazel step sequence is digest-pinned, and a tested event state machine rejects
affected mode for every protected gate. The governed step invokes the root-owned Nix
installation by absolute path, uses a read-only Nix-store Git, requires the exact event
`GITHUB_SHA` in a clean checkout, accepts only the exact `$RUNNER_TEMP` cache contract,
and parses a canonical non-future integer job-start epoch. These repository-local
controls do not replace the pinned organization required workflow that must ultimately
enforce the gate from outside a pull request's mutable trust boundary.

## Rollback

Keep protected events in full mode. Activation requires one coordinated reviewed
change to the blocker contract, event policy, semantic workflow contract, and
orchestration tests after connected evidence exists. Changing only the workflow
argument fails closed.
Retain the exact `bazel / verdict` job name; do not disable the job, weaken failure
behavior, or rename a required context during incident recovery.

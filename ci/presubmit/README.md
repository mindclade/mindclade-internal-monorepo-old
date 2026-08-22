# CI / Presubmit

- **Status:** Implemented; connected check-context and latency qualification remain pending.
- **Owner:** Release Engineering
- **SLO:** [`../../docs/slo/ci-bazel-validation.md`](../../docs/slo/ci-bazel-validation.md)
- **Runbook:** [`../../docs/runbooks/ci-bazel-validation.md`](../../docs/runbooks/ci-bazel-validation.md)

## Responsibility

`pipeline.py` owns the repository-local presubmit contract. `--static-only`
runs architecture policy without a Bazel toolchain. `--bazel-only` selects and
executes configured Bazel validation inside the pinned `.#ci-bazel` shell.

Ordinary pull requests use an explicit base SHA and Bazel's post-loading graph
to calculate package reverse dependencies. Merge groups and protected-main
pushes use full mode. Changes to CI, toolchains, dependency locks, Starlark,
protocols, architecture, component, or maturity policy also force full mode.
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
contract. Activation requires a qualified remote cache, a complete x86_64 Linux
full-graph comparison, a wrapper that restores the checkout after interruption,
and review of the Bazel 9 version-parsing fallback. Until those are satisfied,
the reviewed global-input contract remains the conservative correctness backstop.

Artifact-plan Phase 5 is therefore **incomplete**. The active pull-request path is
still the conservative Bazel-query implementation, while merge groups, protected
main pushes, and nightly runs remain mandatory full-graph gates. Workflow YAML is
parsed structurally by static policy, and a tested event state machine rejects any
attempt to select full mode for a pull request or affected mode for a protected gate.
The governed step invokes the root-owned Nix installation by absolute path, requires
the exact event `GITHUB_SHA` in a clean checkout, and accepts only the generated
bounded-cache `user.bazelrc` contract. These repository-local controls do not replace
the pinned organization required workflow that must ultimately enforce the gate from
outside a pull request's mutable trust boundary.

## Rollback

Revert the affected-selection change, or make one coordinated reviewed change to
the event policy, semantic workflow contract, and orchestration tests so pull
requests resolve to full mode. Changing only the workflow argument fails closed.
Retain the exact `bazel / verdict` job name; do not disable the job, weaken failure
behavior, or rename a required context during incident recovery.

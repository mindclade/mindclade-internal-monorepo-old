# CI / CPU Nightly

- **Status:** Implemented; scheduled connected evidence remains pending.
- **Owner:** Release Engineering
- **SLO:** [`../../docs/slo/ci-bazel-validation.md`](../../docs/slo/ci-bazel-validation.md)
- **Runbook:** [`../../docs/runbooks/ci-bazel-validation.md`](../../docs/runbooks/ci-bazel-validation.md)

## Responsibility

The CPU nightly lane runs complete configured analysis and every non-manual
Bazel test on the default branch. Its committed `targets.yaml` contract is
fail-closed to `//...` for both phases. The workflow is scheduled daily at
05:17 UTC and also supports manual dispatch.

The lane uses the same evidence-producing execution implementation as
presubmit. Workflow YAML owns only scheduling, least-privilege permissions,
pinned tool setup, the 90-minute ceiling, and artifact retention.

## Failure behavior

Loading, toolchain resolution, analysis, testing, evidence normalization, and
contract parsing are blocking. Tests do not run after failed configured
analysis. After affected mode is activated, the 35-day evidence window covers
the 28-day affected-presubmit latency burn-in.

This is CPU repository qualification only. It does not claim GPU, remote
execution, connected providers, release publication, or deployment readiness.

## Retry and rollback

Retry once only when the runbook establishes a transient external-repository
or hosted-runner failure. Repeated failure is repository or dependency drift
and remains red. Roll back the workflow and pipeline together to the last
qualified revision; never replace full mode with an empty or manual target.

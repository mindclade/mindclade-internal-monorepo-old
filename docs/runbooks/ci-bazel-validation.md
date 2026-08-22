# CI Bazel validation runbook

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
   repository fetch, analysis, execution, cache, and critical-path growth.
6. During selector incidents, change the workflow invocation to `--mode full`
   without renaming or disabling `bazel / verdict`. Preserve artifacts and the
   failing revision for follow-up qualification.
7. Governance rollback is a separate reviewed `github-config` change. Return a
   required-check ruleset to evaluate before removing or renaming an emitted
   context.

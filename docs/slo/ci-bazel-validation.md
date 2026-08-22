# CI Bazel validation SLO

## Objective

After a 28-day burn-in, 95% of ordinary pull-request runs whose effective
selection mode remains `affected` complete the `bazel / verdict` job within 30
minutes. Global-fallback, merge-group, protected-main, and nightly full runs are
excluded from this latency percentile and retain hard ceilings of 75, 75, and
90 minutes as applicable.

## Correctness

Selection, Git history, Bazel loading/query, configured analysis, tests, and
evidence normalization fail closed. Correctness is not traded for latency: a
query error or unknown structural change never becomes an empty target set.

## Measurement

`run-metrics.json` records event, effective mode/reason, job elapsed seconds,
target counts, and the 1,800-second objective. Thirty-five-day workflow
retention covers the burn-in window. Cancelled runs, GitHub platform outages,
and full-mode runs are reported separately rather than removed from raw
evidence.

## Promotion gate

The components remain `implemented` until the connected canary matrix passes,
the exact required context is observed on pull requests and merge groups, and
the 28-day p95 objective is calculated from retained evidence.

# CPU nightly Bazel qualification

## Source evidence

The committed contract accepts only full `//...` analysis and tests, rejects
unknown or malformed fields, and delegates execution to the same fail-closed
runner used by presubmit.

## Connected evidence required

Qualification remains pending until scheduled and manual default-branch runs
complete within the 90-minute ceiling, retain complete BEP/profile/selection
artifacts for 35 days, and prove failure reporting for loading, analysis, and
test failures. GPU, provider, remote-execution, and release evidence are
separate lanes.

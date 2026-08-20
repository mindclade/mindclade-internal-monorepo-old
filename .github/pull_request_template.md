## What changed

<!-- The behaviour, not the diff. -->

## Why

<!-- Link the issue, ADR, or runbook. If this changes a boundary, link the ADR. -->

## Gates

Run from the repository root. Tick what you ran; delete what does not apply.

- [ ] `python tools/analysis/run_architecture_checks.py`
- [ ] `python tools/dev/validate_repository.py`
- [ ] `go test -race ./libs/go/... ./control/... ./services/control_plane/...`
- [ ] `uv run pytest`
- [ ] `bazel query '//...'` still loads

## Component maturity

- [ ] No component's status in `components.toml` changed.
- [ ] A status advanced, and it now satisfies every gate `maturity.toml` requires for
      it — owner, stable contract, tests that exist, a Bazel target that is not a
      scaffold `filegroup`, and qualification evidence where the status demands it.

## Ratchets

A ratchet may only move in one direction, and the change that moves it says why.

- [ ] `MATERIALIZATION_BASELINE` (`tests/integration/test_blueprint_scaffold.py`) unchanged.
- [ ] Lowered, and the comment records what closed.
- [ ] `SCAFFOLD_BASELINE` (`tests/integration/test_python_scaffold.py`) unchanged or lowered.

## Boundaries

- [ ] No reusable logic originates under `services/`.
- [ ] Nothing in production depends on `research/`.
- [ ] Every queue, parser, buffer pool, retry loop, and spool this adds is bounded.
- [ ] No gate was weakened, skipped, or allowlisted to make this pass.

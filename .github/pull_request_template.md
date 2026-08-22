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

## Terraform contracts

Delete this section when the change does not touch `infra/terraform`.

- [ ] Generated README/interface evidence is current (`ci/terraform/check.sh docs`).
- [ ] The minimum and reviewed provider matrix passes (`ci/terraform/check.sh compat`).
- [ ] A breaking interface/address change has a SemVer increment, migration record, consumer
      steps, rollback, and qualification evidence; otherwise no breaking change was detected.
- [ ] No plan/apply/state or live IAM mutation is implied by repository-only test evidence.


## Contributor authorization

- [ ] I am authorized under a current written agreement with Mindclade, LLC. to
      submit every part of this contribution.
- [ ] I identified every third-party component, dataset, model, font, media,
      specification, or generated artifact and preserved its source, license,
      provenance, and required notices.
- [ ] I updated `LICENSE`, `NOTICE`, the SBOM, or other license evidence when
      the included or distributed material changed.

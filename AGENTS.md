# Agent operating guide

## Purpose and boundaries

This monorepo owns product, model, data, training, evaluation, serving, platform source, Bazel
build logic, reusable Terraform modules, and immutable release artifacts. Read README.md,
CONTRIBUTING.md, REPOSITORY_STATUS.md, components.toml, maturity.toml, and
docs/architecture/dependency-rules.md before editing.

Materialized paths are not automatically production-ready. Preserve the declared progression
from scaffolded to implemented to qualified to production; never convert a stub or skipped test
into a readiness claim without substantive implementation and evidence.

## Build and dependency rules

- Nix owns pinned host toolchains and execution environments.
- Bazel/Bzlmod owns the authoritative build, test, generation, image, qualification, and release
  graph. Keep BUILD targets narrow and the central layer matrix fail closed.
- Language lockfiles own language dependencies. Do not duplicate those graphs in Nix.
- Production code must not depend on research or experiments. Shared stable contracts belong in
  libs, protocols, schemas, or another declared foundation boundary.
- Schema changes begin in canonical Protobuf/OpenAPI/event sources, then regenerate clients and
  fixtures and run compatibility tests. Never hand-edit generated output.

## Safety

- Do not publish artifacts, create/move release tags, promote GitOps revisions, deploy, or access
  production clusters from an agent session.
- Never commit credentials, kubeconfigs, model weights, private datasets, holdout data, patient
  information, partner data, or generated local caches.
- Preserve user changes in the worktree and avoid broad cleanup commands.

## Validation

Start with:

    tools/dev/nixw develop .#ci --command python3 ci/presubmit/pipeline.py --static-only
    tools/dev/nixw flake check --no-update-lock-file

Then run the smallest owning Bazel target and affected reverse dependencies. Global/toolchain,
schema, and layer changes require the full configured graph and the appropriate connected CI
lane. Provider, GPU, numerical, remote-execution, and release evidence must be reported
separately when unavailable.

## Done

Implementation, tests, Bazel metadata, ownership, documentation, maturity, compatibility,
security, operational limits, and rollback evidence agree. Passing scaffold checks or a local
build alone is never a production claim.

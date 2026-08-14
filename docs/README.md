# Engineering documentation

This directory records the complete Mindclade system design, accepted decisions,
operating procedures, security boundaries, implementation guides,
qualification evidence, and target-state blueprint.

## Reading order

1. `architecture/system-design-reference.md` — canonical cross-system design
2. `architecture/system-design-traceability.md` — design-to-code/evidence map
3. `architecture/system-overview.md` — concise overview
4. `architecture/language-boundaries.md`
5. `architecture/dependency-rules.md`
6. the subsystem chapter relevant to the change
7. accepted ADRs under `design/`
8. component runbooks, security controls, and qualification guidance

## Status language

- **Implemented** means substantive source and meaningful tests/contracts exist
  for the stated boundary.
- **Offline-qualified** means the recorded provider-independent checks passed.
- **Connected qualification required** means provider, cloud, toolchain,
  performance, or deployment evidence is still required.
- **Scaffolded** means ownership/path/contract is reserved but production
  implementation is not claimed.
- **Production** requires implementation plus current qualification, ownership,
  operational expectations, security review, release evidence, and rollback.

`components.toml`, `maturity.toml`, `REPOSITORY_STATUS.md`,
`SCAFFOLD_STATUS.md`, `VALIDATION.md`, and `QUALIFICATION.md` are the status
sources for this artifact.

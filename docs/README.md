<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Documentation site home](index.md) · [Contributing](../CONTRIBUTING.md)

# Engineering documentation

> **Purpose:** The authoritative navigation layer for architecture, decisions,
> implementation guides, operations, security, and qualification evidence.

`docs/` explains how Mindclade's systems fit together and why durable boundaries
exist. Code, tests, configuration, and current qualification records remain the
source of truth for behavior and readiness.

## Documentation map

| Reader goal | Start here |
| --- | --- |
| Understand the system | [`architecture/system-design-reference.md`](architecture/system-design-reference.md) |
| Trace design to code and evidence | [`architecture/system-design-traceability.md`](architecture/system-design-traceability.md) |
| Adopt a supported implementation pattern | [`guides/`](guides/) |
| Understand an accepted decision | [`design/decision-register.md`](design/decision-register.md) |
| Respond to an operational condition | [`runbooks/`](runbooks/) |
| Review security boundaries | [`security/`](security/) |
| Check service objectives | [`slo/`](slo/) |
| Inspect qualification evidence | [`qualification/README.md`](qualification/README.md) |
| Understand the target-state blueprint | [`blueprint/README.md`](blueprint/README.md) |
| Review scale and decomposition triggers | [`roadmap/README.md`](roadmap/README.md) |

## Recommended reading order

1. [Canonical system design](architecture/system-design-reference.md)
2. [Design-to-code traceability](architecture/system-design-traceability.md)
3. [System overview](architecture/system-overview.md)
4. [Language boundaries](architecture/language-boundaries.md)
5. [Dependency rules](architecture/dependency-rules.md)
6. The architecture chapter and accepted ADRs relevant to your change
7. The applicable guide, runbook, security control, SLO, and qualification page

## Status language

| Term | Meaning |
| --- | --- |
| **Implemented** | Substantive source and meaningful tests or contracts exist for the stated boundary. |
| **Offline-qualified** | Recorded provider-independent checks passed. |
| **Connected qualification required** | Provider, cloud, toolchain, performance, or deployment evidence is still required. |
| **Scaffolded** | Ownership, path, or contract is reserved; production implementation is not claimed. |
| **Production** | Implementation, current qualification, ownership, operations, security review, release evidence, and rollback are all present. |

[`components.toml`](../components.toml), [`maturity.toml`](../maturity.toml),
[`REPOSITORY_STATUS.md`](../REPOSITORY_STATUS.md),
[`SCAFFOLD_STATUS.md`](../SCAFFOLD_STATUS.md),
[`VALIDATION.md`](../VALIDATION.md), and
[`QUALIFICATION.md`](../QUALIFICATION.md) are the status sources for this
artifact.

## Build the documentation

From the repository root, enter the pinned environment and run the strict
MkDocs build:

```bash
tools/dev/nixw develop .#default
mkdocs build --strict --config-file docs/mkdocs.yml
```

The generated site is written under `.generated/docs-site/` and must not be
committed.

## Documentation expectations

- Verify commands, defaults, compatibility, and failure behavior from
  repository evidence.
- Link to volatile status facts rather than duplicating them.
- Keep tutorials, task guides, concepts, references, and runbooks distinct.
- Update navigation, operational guidance, and qualification evidence when a
  durable boundary changes.
- Use descriptive links, sequential headings, useful image alt text, and tables
  only for genuinely comparable information.

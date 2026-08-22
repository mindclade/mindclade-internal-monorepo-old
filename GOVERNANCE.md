<!-- mindclade-doc: governance@1 -->

# Mindclade governance · internal monorepo

| Document control | Value |
| --- | --- |
| Owner | Mindclade Engineering |
| Version | 1.0 |
| Last reviewed | August 21, 2026 |
| Authority | Product, platform, model, data, training, serving, and SDK source |

## Decision authority

Each production boundary has a directly responsible owner. Owners approve
changes within the accepted architecture; cross-domain or irreversible changes
require an architecture decision record.

The following always require an ADR:

- a change to language ownership;
- a new deployable service or a service split/merge;
- a new top-level domain;
- a new canonical cross-language wire contract;
- a new persistent store or broker;
- a change to artifact/checkpoint compatibility;
- a new production kernel provider;
- a change to build, toolchain, release, or supply-chain authority;
- an exception to dependency layering or tenant/security isolation.

## Decision lifecycle

```text
proposal -> review -> accepted/rejected -> implementation -> qualification
```

Accepted ADRs are immutable records. A later decision supersedes rather than
silently edits the original rationale. RFCs may evolve during review and use
`docs/design/rfc-template.md`.

## Ownership boundaries

- `libs/` owners maintain reusable mechanisms and conformance tests.
- `control/` owners maintain durable Go domain policy and state machines.
- `services/` owners maintain process composition, resource limits, health,
  drain, and deployment evidence.
- scientific domain owners maintain `data/`, `preprocessing/`, `models/`,
  `training/`, `evaluation/`, and `serving/` semantics.
- platform owners maintain Bazel, Nix, infrastructure, security policy, and
  release evidence.

No team may use a shared package as a shortcut to avoid a clear domain owner.

## Promotion

A scaffold path is not promoted by code review alone. Promotion requires the
local `PRODUCTION_READINESS.md` to link passing tests, SLOs, dashboards, alerts,
runbooks, security review, build provenance, SBOM, signatures, and rollback
proof appropriate to the component.

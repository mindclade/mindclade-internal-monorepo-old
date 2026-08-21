# OpenAPI projections

- **Status:** Public v1 projection implemented; administrative projection remains scaffolded.
- **Primary implementation ownership:** Protobuf, OpenAPI, event schemas, and compatibility policy

## Purpose

Canonical cross-language wire contracts and explicit mappings. A concept may have multiple external projections, but fields have one authority or a tested mapping. This path specializes that domain for **openapi**.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

`public.openapi.yaml` is the stable browser/public-SDK contract for runs,
datasets, models, artifacts, evaluations, and bounded inference. It uses bearer
authentication, idempotency keys on retriable mutations, opaque pagination,
structured problems, and content-addressed scientific payloads.

Change the schema, regenerate with `pnpm run generate`, and run
`pnpm run generate:check`. The administrative projection must remain
non-operational until its independent authorization/audit contract is reviewed.

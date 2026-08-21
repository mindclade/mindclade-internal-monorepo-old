<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture](../docs/README.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Product applications

> **Maturity:** Implemented application surfaces; live identity, API, security,
> accessibility, and deployment qualification remain promotion requirements.
> **Primary implementation:** TypeScript and Next.js.

`apps/` contains Mindclade's product-facing web applications. Applications
consume generated SDKs and public contracts; they do not import deployable
service implementations.

## What's here

| Application | Audience | Responsibility |
| --- | --- | --- |
| [`console/`](console/) | Platform users | Primary product console and scientific workflows |
| [`admin/`](admin/) | Authorized operators | Administrative and operational product surfaces |

Each application owns its composition, routes, and product-specific behavior.
Reusable UI primitives belong in [`libs/ts/design_system/`](../libs/ts/design_system/),
API access belongs in [`libs/ts/api_client/`](../libs/ts/api_client/), and public
client contracts belong in [`sdk/typescript/`](../sdk/typescript/).

## Boundary

- Product surfaces depend on generated SDKs and versioned contracts.
- Server policy and provider wiring stay in [`services/`](../services/).
- Cross-language payloads are defined under [`protocols/`](../protocols/).
- Shared code must have a clear owner and consumer; do not create generic
  `common`, `shared`, `helpers`, or `utils` packages.

## Start here

- Read the [`console` README](console/README.md) or
  [`admin` README](admin/README.md) before changing an application.
- Use [`docs/architecture/dependency-rules.md`](../docs/architecture/dependency-rules.md)
  for allowed dependency direction.
- Check [`SCAFFOLD_STATUS.md`](../SCAFFOLD_STATUS.md) and the application
  `PRODUCTION_READINESS.md` before making readiness claims.

## Promotion bar

Promotion requires a named owner, reviewed contracts, bounded behavior,
meaningful tests, a Bazel target in the pinned Nix environment, documented
failure and rollback behavior, and current production-readiness evidence.

<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture](../docs/README.md) · [Maturity](../SCAFFOLD_STATUS.md)

# SDKs

> **Maturity:** Scaffolded; the checked-in clients are not published or
> production-supported.
> **Primary implementation:** Generated Go, Python, and TypeScript clients.

`sdk/` reserves the public client-library boundary generated from Mindclade's
public contracts, plus narrow ergonomic helpers and examples.

## What's here

| Path | Responsibility |
| --- | --- |
| [`go/`](go/) | Go client surface and provisional module boundary |
| [`python/`](python/) | Python client package boundary |
| [`typescript/`](typescript/) | TypeScript client package boundary |
| [`examples/`](examples/) | Cross-language client usage examples |

## Boundary

- SDKs expose supported public contracts; they do not contain server, policy,
  provider, or workflow implementation.
- Generated types originate in [`protocols/`](../protocols/).
- Handwritten helpers must remain narrow and must not fork wire semantics.
- Language support windows, package coordinates, authentication, pagination,
  retries, and error behavior become promises only when explicitly documented
  and qualified.

## Start here

- Read the language-specific README before changing a client:
  [`Go`](go/README.md), [`Python`](python/README.md), or
  [`TypeScript`](typescript/README.md).
- Use [`examples/README.md`](examples/README.md) for the reserved example
  surface.
- Read [ADR-0014](../docs/design/adr-0014-protocol-authority.md) before changing
  generated API semantics.

## Promotion bar

Publishing requires a stable generated API, chosen compatibility and support
windows, authentication and error contracts, conformance tests, package
provenance, release automation, migration guidance, and current qualification.

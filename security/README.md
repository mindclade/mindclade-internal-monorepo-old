<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Security policy](../SECURITY.md) · [Threat model](../docs/security/threat-model.md)

# Security policy inputs

> **Purpose:** Machine-readable security and supply-chain policy owned at the
> repository root.

`security/` contains repository-wide policy inputs that do not belong to one
deployment provider or application package.

## What's here

| File | Responsibility |
| --- | --- |
| [`rust-supply-chain.toml`](rust-supply-chain.toml) | Rust dependency and software-supply-chain policy inputs |

## Boundary

- Private vulnerability reporting and response policy lives in
  [`SECURITY.md`](../SECURITY.md).
- Threat models and conceptual controls live under
  [`docs/security/`](../docs/security/).
- Deployable infrastructure controls live under
  [`infra/security/`](../infra/security/).
- Verification tooling and evidence live under
  [`tools/qualification/security/`](../tools/qualification/security/) and the
  applicable qualification records.

## Start here

- [Threat model](../docs/security/threat-model.md)
- [Tenant isolation](../docs/security/tenant-isolation.md)
- [Execution-ticket security](../docs/security/execution-ticket-security.md)
- [Supply-chain security](../docs/security/supply-chain.md)

Never commit credentials, private datasets, model-weight secrets, hidden
evaluation material, patient information, or proprietary partner data.

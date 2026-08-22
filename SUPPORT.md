<!-- mindclade-doc: support@1 -->

# Mindclade support · internal monorepo

| Document control | Value |
| --- | --- |
| Owner | Mindclade Engineering |
| Version | 1.0 |
| Last reviewed | August 21, 2026 |

## Routing

| Need | Route |
| --- | --- |
| Product, SDK, model, data, or platform defect | Open a sanitized issue with the owning component and revision |
| Design or cross-boundary proposal | Open an RFC or ADR through the process in [GOVERNANCE.md](GOVERNANCE.md) |
| Build or qualification failure | Follow the relevant guide under `docs/qualification/` and attach command-level evidence |
| Production incident | Use the owning component runbook and incident-response process |
| Security, privacy, isolation, supply-chain, or biosecurity concern | Follow [SECURITY.md](SECURITY.md); never open an issue |
| Cloud or GitOps desired state | Route to `mindclade/infrastructure-live` or `mindclade/gitops` |
| Contractual customer support | Use the channel and service terms in the applicable agreement |

GitHub issues do not carry an SLA. Never include credentials, customer or
patient data, private datasets, model weights, hidden evaluation material,
restricted biological content, or incident-sensitive evidence. Prefer
sanitized request, run, trace, release, artifact, or evidence identifiers.

Only components with completed production-readiness evidence are supported as
production deployments. The applicable customer agreement or incident-response
process controls if it conflicts with this routing guide.

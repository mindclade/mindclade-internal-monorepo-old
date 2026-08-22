# Terraform environment roots

This repository has no deployable Terraform roots. Public domain delegation and every
environment composition are owned by the separately controlled live configuration. The
former `dns_hub` composition is retained only as a module test fixture under
`modules/dns/tests/fixtures/dns_hub`; it has no backend, component registration, lock, or
apply workflow and cannot become a second public-zone state owner.

`development`, `staging`, and `production` are reserved target names, not safe empty
roots and not evidence of deployed environments. Their live compositions belong in
the separately controlled infrastructure configuration once hierarchy, state,
identity, regions, IPAM, retention, compliance, SLO, RTO/RPO, and cost decisions are
approved. Do not run Terraform from those directories until their scaffold marker is
replaced by a reviewed root contract, partial backend, constraints/lock file, tests,
and `PRODUCTION_READINESS.md` evidence.

The required dependency and promotion model is documented in the parent
[`README.md`](../README.md) and [`PRODUCTION_READINESS.md`](../PRODUCTION_READINESS.md).

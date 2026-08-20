# Terraform environment roots

`dns_hub` is the only deployable root in this repository. It is split by lifetime:
public domain delegation exists before and outlives every development, staging, or
production VPC and cluster.

`development`, `staging`, and `production` are reserved target names, not safe empty
roots and not evidence of deployed environments. Their live compositions belong in
the separately controlled infrastructure configuration once hierarchy, state,
identity, regions, IPAM, retention, compliance, SLO, RTO/RPO, and cost decisions are
approved. Do not run Terraform from those directories until their scaffold marker is
replaced by a reviewed root contract, partial backend, constraints/lock file, tests,
and `PRODUCTION_READINESS.md` evidence.

The required dependency and promotion model is documented in the parent
[`README.md`](../README.md) and [`PRODUCTION_READINESS.md`](../PRODUCTION_READINESS.md).


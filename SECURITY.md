# Security policy

## Reporting

Report suspected vulnerabilities, credential exposure, tenant-isolation
failures, model-weight access anomalies, supply-chain compromise, or biological
safety control failures through Mindclade's private security channel. Do not
open a public issue containing exploit details, secrets, sensitive inputs, or
customer information.

Include the affected component, observed behavior, minimal reproduction,
impact, timestamps, request/run identifiers, and any relevant evidence digest.
Do not copy confidential data into the report when an immutable artifact
reference is sufficient.

## Security architecture

The repository uses these primary boundaries:

- Go owns durable identity, tenancy, policy, audit, grants, and revocation.
- Rust validates signed execution authority locally and owns the online/node
  data plane.
- Python workers receive only bounded, scoped execution inputs and artifact
  capabilities.
- content-addressed artifacts are verified before use and published atomically.
- production images, model bundles, toolchains, and release evidence are built
  and signed through the Bazel/Nix supply-chain path.

See `docs/security/` for the threat model, execution-ticket security, model
weight controls, tenant isolation, and supply-chain requirements.

## Minimum coding requirements

- no secrets in source, logs, errors, metrics, traces, or test fixtures;
- no externally returned `err.Error()` strings;
- no unbounded queues, bodies, parser allocation, retries, or shutdown waits;
- no stale fencing token may mutate durable state or publish output;
- no production process may start with in-memory providers or scaffold
  factories;
- no model weight or protected artifact is accessed without an auditable grant;
- all untrusted inputs are size-bounded and validated before allocation;
- all dependency and image updates pass provenance and vulnerability checks.

## Supported scope

Only components with completed `PRODUCTION_READINESS.md` evidence are supported
as production deployments. The presence of a scaffold file does not imply a
supported implementation.

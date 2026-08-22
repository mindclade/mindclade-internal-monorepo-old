<!-- mindclade-doc: security@1 -->

# Mindclade security policy · internal monorepo

| Document control | Value |
| --- | --- |
| Owner | Mindclade Security |
| Version | 1.0 |
| Last reviewed | August 21, 2026 |
| Repository scope | Product, platform, model, data, training, serving, and SDK source |

## Reporting

Report suspected vulnerabilities, credential exposure, tenant-isolation
failures, model-weight access anomalies, supply-chain compromise, or biological
safety control failures through a private channel. Do not open an issue or
discussion containing exploit details, secrets, sensitive inputs, or customer
information.

| Channel | Use it for |
| --- | --- |
| [Private security advisory](https://github.com/mindclade/mindclade-internal-monorepo/security/advisories/new) | Preferred for a vulnerability in this repository |
| `security@mindclade.com` | Reports that cannot be submitted through GitHub; ordinary email is not end-to-end encrypted |
| `biosecurity@mindclade.com` | Screening bypasses, unsafe generations, or dual-use model behavior |

The organization-wide scope, response targets, safe harbor, and coordinated-
disclosure policy are defined in
[`mindclade/.github/SECURITY.md`](https://github.com/mindclade/.github/blob/main/SECURITY.md).
Its response times are operational targets, not contractual service levels. Its
safe harbor applies only within the canonical scope and does not authorize
third-party systems or data, promise a bounty, or excuse unlawful conduct.

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

## Proprietary and third-party license controls

Tracked first-party source and policy files use the canonical block in
`.github/MINDCLADE_PROPRIETARY_SOURCE_HEADER.txt` and resolve
`SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary` to the root
[LICENSE](LICENSE). Validate the tree with:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 tools/license/check_license_headers.py --check
tools/dev/nixw develop .#ci --command cargo deny check licenses
```

The header checker deliberately excludes independently licensed agent skills,
vendored content, generated clients, and machine-owned lock/reference files.
Those materials retain their own terms and must remain traceable through
[NOTICE](NOTICE), manifests, lockfiles, SBOMs, provenance, and accompanying
license texts. Never add a Mindclade ownership header to third-party material.

## Supported scope

Only components with completed `PRODUCTION_READINESS.md` evidence are supported
as production deployments. The presence of a scaffold file does not imply a
supported implementation.

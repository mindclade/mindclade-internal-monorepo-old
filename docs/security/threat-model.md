# Mindclade platform threat model

## Scope

The platform ingests untrusted external scientific data, stores valuable
proprietary datasets/model weights, runs user- and system-initiated GPU/CPU
workloads, exposes public and administrative APIs, and crosses Go, Rust,
Python, TileLang, TypeScript, Kubernetes, object storage, PostgreSQL, brokers,
and build/release systems.

## Trust boundaries

1. Public client/SDK to public API.
2. Go control plane to Rust runtime/data plane.
3. Rust runtime host/node agent to Python model/scientific workers.
4. Worker to artifact/reference stores.
5. Build system to released image/model/evidence bundle.
6. Tenant/workspace to another tenant/workspace.
7. Administrative operator and break-glass paths.
8. External data/reference sources and external search tools.

## Primary threats and controls

| Threat | Required controls |
|---|---|
| Cross-tenant access | tenant-scoped authz, signed artifact grants, namespace enforcement, audit |
| Forged/replayed work | signed expiring tickets, nonce/ID, policy epoch, resource budget, fencing |
| Stale worker commit | lease/ticket fencing token and atomic manifest/status precondition |
| Untrusted bytes/parser abuse | bounded Rust parsers, input/allocation/nesting limits, fuzzing, process isolation |
| SSRF/webhook abuse | `httpx/outbound` allowlists, DNS/IP checks, redirect revalidation, byte limits |
| Supply-chain compromise | Bazel/Nix pinning, Bzlmod lock, SBOM, provenance, signed images/bundles |
| Model-weight exfiltration | brokered scoped access, short-lived grants, node/workload identity, audit |
| Artifact substitution/corruption | content digests, signed manifests, conditional commit, verification before use |
| Policy bypass during outage | bounded cached authority, snapshot expiry, fail-closed admission, revocation epoch |
| Privilege escalation on cluster | least privilege, workload identity, network/admission policy, no host access |
| Sensitive telemetry leakage | field classification, redaction, bounded safe fault/audit records |
| Unsafe kernel/compiler result | PyTorch reference, signature-specific qualification, revocation/fallback |

## Security invariants

- No unauthenticated mutation or unscoped artifact access.
- No stale fencing token may commit state or artifacts.
- No unsigned or expired execution authority is accepted.
- No production queue, parser allocation, response body, or telemetry spool is
  unbounded.
- No production release bypasses evidence and signature verification.
- No model or scientific input is written to logs by default.

Each promoted service updates this model with concrete data flows, credentials,
ports, storage, dependencies, abuse cases, and evidence links.

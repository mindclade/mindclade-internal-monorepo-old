# MLflow production readiness

**Decision:** implementation present; activation blocked; not approved for cluster reconciliation.
**Review date:** 2026-08-23.
**Target upstream:** MLflow 3.15.1.

Evidence classes are explicit: **Observed** means repository or command evidence was inspected;
**Inferred** is a conclusion from observed evidence; **Proposed** is desired state not yet applied;
**Unknown** requires cloud or connected-cluster evidence.

| Class | Gate | Status | Evidence or blocker |
|---|---|---|---|
| Observed | Default chart render | PASS | zero resources with repository defaults |
| Observed | Qualified structural render | PASS | eight namespaced resources from the non-live fixture |
| Observed | Kubernetes security policy | PASS | restricted contexts, digest image, no API token, no Secret/RBAC/PVC/public Service |
| Observed | Namespace isolation | PASS | restricted PSS, zero Pod/object quota, namespace-wide ingress/egress deny |
| Observed | Artifact access model | PASS | proxied `MLFLOW_ARTIFACTS_DESTINATION`; clients need no GCS credentials |
| Observed | Workspace/auth model | PASS | workspaces and basic-auth enabled; external fail-closed RBAC config required |
| Observed | Gateway authority isolation | PASS | native MLflow Gateway budget/serving configuration is absent; anonymous telemetry is disabled |
| Observed | Database upgrade ordering | PASS | single PreSync Job gates rollout and keeps the URI out of manifest argv |
| Observed | Runtime dependency graph | PASS | separate hash lock includes explicit auth/genai, PostgreSQL, GCS, upstream Redis-client compatibility, and uvicorn-standard roots |
| Observed | Patched binary dependency floors | PASS | the hash lock carries Pillow 12.3.0 and PyArrow 25.0.1; direct minimums prevent clean regeneration from restoring their vulnerable ranges |
| Observed | Cryptography advisory remediation | BLOCKED | The Security workflow audits the independent MLflow lock and validates it against `services/mlflow/security-gate.json`; `pip-audit` reports PYSEC-2026-3552: MLflow 3.15.1 requires `cryptography<50`, while the first patched release is 50.0.0. No override or exception is approved, the blocker expires into a CI failure on 2026-09-07, and image promotion remains blocked until an upstream-compatible MLflow release is qualified |
| Observed | Target platform | PASS | Linux CI binds the OCI digest; every host checks Linux/amd64, non-root identity, entrypoint, required extras, and Linux native extensions |
| Observed | Trace privacy contract | PASS | Python exporter sends digest identity and bounded attributes, never inputs/outputs |
| Inferred | CRD/operator need | NOT JUSTIFIED | upstream server and namespaced resources cover the requirement |
| Proposed | Cloud SQL | BLOCKED | no observed instance, database, TLS, private CIDR, PITR, restore, or connection budget |
| Proposed | Artifact/archive buckets | BLOCKED | no observed bucket, IAM, retention, encryption, lifecycle, or restore evidence |
| Proposed | Workload Identity | BLOCKED | no observed dedicated GSA, KSA binding, IAM policy, or access test |
| Proposed | Runtime Secret | BLOCKED | no observed secret-store object, synchronization mechanism, or rotation test |
| Proposed | TLS/identity ingress | BLOCKED | no qualified Gateway/certificate/identity-proxy ownership path |
| Unknown | Image runtime | BLOCKED | Linux container smoke, SBOM/signature/provenance/vulnerability evidence not attached |
| Unknown | Database migration | BLOCKED | staging snapshot, upgrade duration, compatibility, and restore exercise absent |
| Unknown | Workspace isolation | BLOCKED | cross-workspace negative tests and admin bootstrap/rotation evidence absent |
| Observed | Governed Gateway source | PASS | Go two-person bundles and durable reservation lifecycle plus the Rust proxy's exact OpenAI-compatible routes are implemented outside MLflow |
| Unknown | Governed Gateway deployment | BLOCKED | IAP audience, caller identity, TLS-inspecting Secure Web Proxy, provider reconciliation, failover, and measured SLO evidence absent |
| Unknown | SLO/scale | BLOCKED | measured latency/error/saturation, connection capacity, HPA response, and load results absent |
| Unknown | Recovery | BLOCKED | SQL PITR, GCS restore, governed Gateway reconciliation, trace rehydration, and rollback drills absent |
| Unknown | GitOps | BLOCKED | monorepo release, release selection, generated manifests, server-side diff, and empty drift absent |

## Release gate

Activation requires one immutable evidence graph that binds:

- the monorepo revision, Linux/amd64 image digest, dependency lock digest, SBOM, provenance,
  signature, vulnerability decision, and rollback image;
- chart, environment values, rendered manifest, and policy result digests;
- Cloud SQL schema snapshot/migration result, GCS dependency identities, and secret
  metadata version (never secret values);
- workspace/RBAC negative tests, artifact and dataset lineage tests, evaluation threshold evidence,
  trace redaction tests, governed Gateway budget/guardrail tests, and model source validation tests;
- load, disruption, database failover, Gateway reconciliation/failover, backup restore, rollout, rollback, alert,
  and empty post-sync drift evidence.

The release is rejected if any required node is missing, stale, mutable, for a different subject,
or disconnected from the exact image/render subject. MLflow's registry cannot self-approve this
gate; the Go release and lineage services remain authoritative.

The blocked dependency state is executable, not prose-only. `runtime.lock.yaml` carries
`publicationState: blocked-security-findings`, the chart defaults render nothing, MLflow is absent
from the closed release target catalog, and `validate_dependency_gate.py --require-clean` rejects
the current scanner result. Removing any one of those boundaries fails the repository gate. The
security record is a denial with a remediation deadline; it is not a vulnerability exception.

## SLO acceptance

Numerical objectives must be measured in staging before production values are chosen. At minimum
define availability, request-error rate, tracking p95/p99 latency, artifact upload/download
latency and failure rate, governed Gateway added latency, migration duration, trace archival lag,
SQL pool saturation, GCS error rate, HPA saturation, and recovery time/objectives.
Alert rules must page on user impact and sustained exhaustion, not raw CPU alone.

Production approval requires named owners from ML platform, database, storage, security, GitOps,
and on-call, plus a dated exception for every residual risk. There are no approved exceptions.

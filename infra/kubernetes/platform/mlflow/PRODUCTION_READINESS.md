# MLflow production readiness

**Decision:** implementation present; activation blocked; not approved for cluster reconciliation.  
**Review date:** 2026-08-21.  
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
| Observed | Gateway budget consistency | PASS | Redis URI is mandatory and network egress is explicit |
| Observed | Database upgrade ordering | PASS | single PreSync Job gates rollout and keeps the URI out of manifest argv |
| Observed | Runtime dependency graph | PASS | separate hash lock includes explicit auth/genai, PostgreSQL, GCS, Redis, and uvicorn-standard roots |
| Observed | Target platform | PASS | OCI lock test proves Linux/amd64, non-root identity, entrypoint, required extras, and Linux native extensions |
| Observed | Trace privacy contract | PASS | Python exporter sends digest identity and bounded attributes, never inputs/outputs |
| Inferred | CRD/operator need | NOT JUSTIFIED | upstream server and namespaced resources cover the requirement |
| Proposed | Cloud SQL | BLOCKED | no observed instance, database, TLS, private CIDR, PITR, restore, or connection budget |
| Proposed | Artifact/archive buckets | BLOCKED | no observed bucket, IAM, retention, encryption, lifecycle, or restore evidence |
| Proposed | Redis | BLOCKED | no observed HA TLS endpoint, persistence/failover test, or exact private CIDR |
| Proposed | Workload Identity | BLOCKED | no observed dedicated GSA, KSA binding, IAM policy, or access test |
| Proposed | Runtime Secret | BLOCKED | no observed secret-store object, synchronization mechanism, or rotation test |
| Proposed | TLS/identity ingress | BLOCKED | no qualified Gateway/certificate/identity-proxy ownership path |
| Unknown | Image runtime | BLOCKED | Linux container smoke, SBOM/signature/provenance/vulnerability evidence not attached |
| Unknown | Database migration | BLOCKED | staging snapshot, upgrade duration, compatibility, and restore exercise absent |
| Unknown | Workspace isolation | BLOCKED | cross-workspace negative tests and admin bootstrap/rotation evidence absent |
| Unknown | Gateway governance | BLOCKED | endpoint allowlist, provider identity, rejecting budgets, guardrails, and failure tests absent |
| Unknown | SLO/scale | BLOCKED | measured latency/error/saturation, connection capacity, HPA response, and load results absent |
| Unknown | Recovery | BLOCKED | SQL PITR, GCS restore, Redis failover, trace rehydration, and rollback drills absent |
| Unknown | GitOps | BLOCKED | monorepo release, release selection, generated manifests, server-side diff, and empty drift absent |

## Release gate

Activation requires one immutable evidence graph that binds:

- the monorepo revision, Linux/amd64 image digest, dependency lock digest, SBOM, provenance,
  signature, vulnerability decision, and rollback image;
- chart, environment values, rendered manifest, and policy result digests;
- Cloud SQL schema snapshot/migration result, GCS and Redis dependency identities, and secret
  metadata version (never secret values);
- workspace/RBAC negative tests, artifact and dataset lineage tests, evaluation threshold evidence,
  trace redaction tests, Gateway budget/guardrail tests, and model source validation tests;
- load, disruption, database failover, Redis failover, backup restore, rollout, rollback, alert,
  and empty post-sync drift evidence.

The release is rejected if any required node is missing, stale, mutable, for a different subject,
or disconnected from the exact image/render subject. MLflow's registry cannot self-approve this
gate; the Go release and lineage services remain authoritative.

## SLO acceptance

Numerical objectives must be measured in staging before production values are chosen. At minimum
define availability, request-error rate, tracking p95/p99 latency, artifact upload/download
latency and failure rate, Gateway added latency, migration duration, trace archival lag, SQL pool
saturation, Redis error/latency, GCS error rate, HPA saturation, and recovery time/objectives.
Alert rules must page on user impact and sustained exhaustion, not raw CPU alone.

Production approval requires named owners from ML platform, database, storage, security, GitOps,
and on-call, plus a dated exception for every residual risk. There are no approved exceptions.

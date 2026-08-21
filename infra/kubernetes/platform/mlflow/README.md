# MLflow platform mirror

This package deploys upstream MLflow as a workspace-isolated metadata, experiment, evaluation,
trace, and AI Gateway mirror. Mindclade CAS manifests, dataset snapshots, Go release/lineage
records, signed execution tickets, and the Rust serving path remain authoritative. An MLflow run,
alias, endpoint, or artifact URI cannot authorize a release or deployment.

Repository defaults are deliberately inert: `activation.enabled=false` renders zero resources.
`qualification-values.yaml` is a non-live structural fixture. A real environment values file may
exist only in a protected monorepo release and must be selected by GitOps together with the exact
image digest and release evidence.

## Runtime shape

- Two or more non-root, read-only server replicas behind a ClusterIP Service.
- SQL tracking, registry, workspace, auth, Gateway, and evaluation metadata in PostgreSQL.
- GCS artifacts and trace archives proxied through the server, so clients receive no bucket
  credentials. MLflow resolves the workspace-aware proxy root to `mlflow-artifacts:/`.
- Basic auth plus workspace RBAC, with explicit host/CORS protection.
- Redis-backed AI Gateway budget enforcement shared across every worker and replica.
- A PreSync, bounded, single-writer schema migration Job before the Deployment rollout.
- GKE Workload Identity, no Kubernetes API token, namespace default deny, exact network flows,
  PDB, HPA, topology spread, probes, and GMP `PodMonitoring`.
- No Secret, Namespace, RBAC, PVC, CRD, Gateway, certificate, or public Service is created here.

The image at `//services/mlflow:image` is assembled with Bazel/rules_oci from an independently
hash-locked Python graph. A platform transition forces Linux/amd64 wheels even when a developer
builds on macOS. The image contains MLflow 3.15.1 with basic-auth, AI Gateway, PostgreSQL, GCS, and
Redis support. GitOps must replace the chart image with the protected release record's exact
Artifact Registry digest.

## External prerequisites

Activation must bind observed, environment-specific resources rather than fabricate them:

1. a private, highly available PostgreSQL instance, database, TLS policy, backup/PITR policy,
   migration owner, connection ceiling, and exact private address;
2. a versioned GCS bucket prefix for proxied artifacts and another prefix for trace archival,
   with uniform bucket access, retention/lifecycle, encryption, restore evidence, and no public
   access;
3. a highly available TLS Redis endpoint for shared Gateway budget state;
4. a dedicated GSA with only required object permissions and a qualified KSA/GSA binding;
5. an externally synchronized `mlflow-runtime` Secret and rotation owner;
6. an identity-aware TLS ingress namespace and hostname permitted by the GitOps project;
7. exact private database/Redis CIDRs and approved Google API restricted-VIP CIDRs;
8. image SBOM, provenance, signature, vulnerability result, runtime smoke result, and rollback
   digest connected by one release evidence graph.

The external Secret must provide exactly these chart-selected keys:

| Key | Contract |
|---|---|
| `backend-store-uri` | TLS PostgreSQL SQLAlchemy URI; never emitted in a manifest or process argv |
| `flask-secret-key` | stable high-entropy CSRF/encryption key shared by all replicas |
| `gateway-budget-redis-uri` | authenticated `rediss://` URI for shared budget state |
| `auth-config.ini` | MLflow basic-auth/RBAC configuration |
| `trace-archival.yaml` | server-owned bounded trace archival policy |

The auth configuration must use a durable SQL `database_uri`,
`default_permission = NO_PERMISSIONS`, and `grant_default_workspace_access = false`. Bootstrap
administrator credentials are delivered and rotated through the secret system; they are never
stored here. Every non-admin user receives an explicit workspace role. Broad workspace grants
cannot be narrowed with an explicit deny, so roles must be built from narrow grants.

The trace archival configuration must be enabled, use a real `gs://` archive location, declare a
finite retention, set a bounded `max_traces_per_pass`, and keep the long-retention allowlist empty
unless a reviewed compliance requirement names exact experiment IDs.

## Qualification

Run the repository gate; it inventories this chart, proves the default render is empty, renders
the non-live fixture, validates schemas and cross-resource references, rejects Secrets/RBAC/CRDs
and external Services, and applies the Kubernetes policy suite:

```sh
nix develop .#ci --command tools/dev/bazelw test \
  //infra/kubernetes:validate --test_output=errors
```

The image also needs an amd64 runtime test that imports `flask_wtf`, `google.cloud.storage`,
`psycopg2`, `redis`, and MLflow Gateway modules, reports MLflow 3.15.1, starts against disposable
PostgreSQL/GCS-compatible/Redis dependencies, executes `mindclade-db-upgrade`, and probes
`/version`, `/health`, `/metrics`, workspace isolation, artifact proxying, trace archival, and a
rejecting budget. Static and image-build success alone do not satisfy that connected gate.

No custom resource is justified for MLflow. The upstream APIs and Git-owned Helm values express
the desired state without introducing a new cluster-scoped API, controller, finalizer, or webhook.

See [PRODUCTION_READINESS.md](PRODUCTION_READINESS.md) and [RUNBOOK.md](RUNBOOK.md).

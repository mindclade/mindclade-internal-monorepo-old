# MLflow integration

Mindclade integrates with MLflow as an optional discovery and comparison mirror. Mindclade's
content-addressed artifact manifests, dataset snapshots, signed execution tickets, model
descriptors, Go lineage/release evidence, and Rust serving path remain authoritative. MLflow run
IDs, aliases, endpoint names, and artifact paths are never substitutes for those identities.

## Tracking and lineage

The Python exporter uses the low-level `MlflowClient` shape, not global active-run state. It
serializes calls, bounds fields and references, performs synchronous writes, and logs canonical
`mindclade/lineage.json` containing exact source revision, runtime image, attempt/resume,
configuration, dataset, model, and artifact digests. If the lineage artifact write fails, the
partial MLflow run is terminated as failed and never becomes the active mirror run.

The default mode is optional: an MLflow outage increments a local failure counter but does not
interrupt authoritative training or serving. A workflow that makes the mirror a release
requirement must explicitly select `required=True` and qualify that outage behavior.

Dataset records always carry immutable snapshot/manifest digests and declared roles; a mutable
path or MLflow dataset name alone is not lineage. Evaluation jobs use predeclared absolute,
baseline-relative, sample-count, and slice gates. They may project deterministic evidence into
MLflow, but only the Go lineage/release boundary can verify the connected evidence graph and
approve promotion.

## GenAI tracing

The trace exporter uses explicit low-level trace/span lifecycles. It exports content-addressed
identity plus bounded `mindclade.*` attributes only. Request inputs, model outputs, prompts,
messages, credentials, and other payload fields are always `None`; sensitive-looking attribute
keys and control characters are rejected. Child spans must end before their trace, foreign
handles are rejected, and active trace/span counts are bounded.

This contract prevents accidental raw-payload export. It does not replace domain-specific
redaction before diagnostic attributes are constructed. Restricted traces remain restricted even
when MLflow stores only digest identity.

## Server and workspaces

Production server requirements are encoded under `infra/kubernetes/platform/mlflow/`:

- build the hash-locked Mindclade wrapper image with MLflow auth/GenAI, PostgreSQL, GCS, and Redis
  support;
- use durable PostgreSQL and proxied GCS artifacts so clients do not hold bucket credentials;
- use basic-auth workspaces with `default_permission = NO_PERMISSIONS`,
  `grant_default_workspace_access = false`, and explicit narrow roles;
- use bounded GCS trace archival and Redis-backed Gateway budgets across workers and replicas;
- configure explicit hosts/CORS, TLS identity ingress, restricted pods, exact network flows,
  migration ordering, probes, PDB/HPA/topology, metrics, alerts, and backups;
- test schema upgrade, credential rotation, cross-workspace denial, dependency failover, restore,
  rollout, and rollback before promotion.

The chart renders nothing by default and creates no Secret, RBAC, PVC, CRD, certificate, Gateway,
or public Service. Cloud dependencies, identities, secrets, TLS ownership, and exact CIDRs must be
observed and supplied by an activation bundle; placeholders are never cluster intent.

## AI Gateway and deployment metadata

AI Gateway administration is governed control-plane work. Every endpoint needs an approved
workspace, provider/model allowlist, credential owner, usage tracing, rejecting budget, guardrail
policy where required, and rollback record. Upstream endpoint changes take effect dynamically, so
production edits require the same two-person review and evidence capture as a deployment.

MLflow model serving and AI Gateway are not the Mindclade online serving authority. Models are
admitted by signed Mindclade descriptors and tickets and run through the Rust gateway/host plus
Python model worker. Any mirrored deployment record must point to the immutable Mindclade
model/runtime digests and cannot bypass safety, rollout, capacity, or release gates.

The chart remains absent from active GitOps inventory until a protected monorepo release, exact
image/release record, cloud dependencies, staging qualification, and reviewed generated output all
exist. See the package production-readiness report and operations runbook before proposing
activation.

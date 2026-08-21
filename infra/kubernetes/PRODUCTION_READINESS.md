# Kubernetes production readiness

**Decision:** foundation implemented; workload activation blocked; not approved
for production apply.  
**Last repository review:** 2026-08-20.  
**Contract version:** `1`, as recorded in `versions.env`.

## Current evidence

| Gate | Status | Evidence or blocker |
|---|---|---|
| Development/staging/production roots render | PASS | Kustomize roots under `overlays/` |
| Restricted Pod Security is explicit and version-pinned | PASS | `base/namespace.yaml` |
| Default identity has no API token | PASS | `base/service-accounts.yaml` |
| Suspended workload identity has no API token or RBAC | PASS | `mindclade-workload` in `base/service-accounts.yaml` |
| Foundation RBAC is least privilege | PASS | No grants exist in `base/rbac.yaml` |
| Default-deny networking | PASS | DNS is the only allowed egress in `policies/network-policies.yaml` |
| Workload activation is fail-closed | PASS | Pod and workload-object quotas are zero in every overlay |
| Workload capacity domains are internally consistent | PASS | Separate CPU/H100/B200 namespaces, LimitRanges, queues, and coherent resource-flavor groups |
| GPU packing contracts exist | PASS | Suspended one-GPU packed and eight-GPU full-node templates for H100 and B200 |
| Secrets are absent | PASS | Foundation contains only non-secret deployment identity |
| Static render/schema/policy CI | PASS | Bazel receives declared tools and fixed-hash core/custom schemas; CI executes it in the pinned Nix closure |
| Required merge enforcement | EXTERNAL | Stable `infra-static` check exists; the GitHub ruleset must require that context |
| Validator tool repository | TRANSITIONAL | Actions declare and stage inputs, but CLI paths are captured from the pinned Nix shell during Bazel repository evaluation; replace with platform-specific fixed-output tool repositories |
| Named component owner and security review | MISSING | Infra component/maturity records and required approvals |
| GitOps reconciliation and drift evidence | MISSING | Approved Application/ApplicationSet, diff, sync, and rollback evidence |
| Workload identities and RBAC | BLOCKED | Must be derived per runnable service and observed API calls |
| Workload images and supply-chain evidence | BLOCKED | Digest-pinned OCI image, SBOM, signature, provenance, and admission result per service |
| Network allowlists | BLOCKED | Exact DNS/API/database/cache/broker/object-store/telemetry flows per service |
| Availability and scaling | BLOCKED | Probes, drain, PDB, topology spread, autoscaling, capacity, and disruption tests |
| Operator transaction and CRD retention | PARTIAL | Byte-locked distinct CRD/controller phases, no-prune CRDs, server-side apply, sync order, and paused GitOps are declared; connected install/upgrade evidence is missing |
| Queue and GPU/RDMA/NCCL activation | BLOCKED | Queues are held with zero quota; hardware, topology, transport, checkpoint, and rollback qualification is missing |
| Operator observability source contract | PARTIAL | GMP PodMonitoring and namespaced recording Rules exist; external rotating auth/trust, exact RBAC, collector network access, and durable JobSet outcome telemetry remain blocked |
| Observability operation and MLOps | BLOCKED | Collector reachability, target/rule health, Cloud Monitoring policies/channels, SLO tests, and data/model-quality evidence |
| Connected non-production qualification | MISSING | Server-side validation, rollout, failure injection, and recovery exercise |

An offline render proves syntax and composition only. It does not prove API
availability, admission-controller behavior, cloud IAM, quotas, capacity,
network reachability, image admission, storage durability, or SLO compliance.

## Workload activation gate

Activation requires one reviewed change that names the target environment and
contains all of the following:

1. a runnable service release pinned by OCI digest, with SBOM, provenance,
   signature, vulnerability policy, and rollback digest;
2. a dedicated ServiceAccount with token automount disabled unless Kubernetes
   API access is demonstrated, plus qualified workload-identity binding;
3. exact Role/RoleBinding rules derived from API behavior, with no wildcard,
   `escalate`, `bind`, or `impersonate` permissions;
4. a restricted container and Pod security context, seccomp, dropped
   capabilities, read-only root filesystem where supported, and bounded
   writable volumes;
5. measured requests/limits, startup/readiness/liveness probes, bounded drain
   and termination grace, rollout strategy, PDB, and topology policy;
6. explicit ingress and egress NetworkPolicies with service/port ownership;
7. secret references and rotation ownership without repository secret values;
8. SLI/SLO, alert, dashboard, log-redaction, runbook, and on-call ownership;
9. a content-addressed capacity contract based on minimum guaranteed
   schedulable capacity, plus matching Kubernetes and Kueue quota changes for
   only the required class;
10. successful offline validation, server-side dry-run/diff in an isolated
    non-production cluster, rollout/rollback, disruption, and security tests.

The namespace activation annotation and ConfigMap value must change in the same
review as the quota and workloads. Removing only `pods: "0"` is not activation.

## CRD and controller gate

CRDs are cluster-scoped and are never installed as an incidental workload
dependency. Kueue, JobSet, cert-manager, Gateway API, observability, and policy
controllers require:

- an immutable upstream version and byte/digest provenance;
- CRD-before-controller-before-custom-resource sync ordering;
- stored-version, conversion, and backward-compatibility review;
- least-privilege controller RBAC and webhook failure-policy review;
- upgrade skew, rollback, backup, and deletion/conversion testing;
- an explicit owner for cluster-wide availability and security impact.

The repository's JobSet, Kueue, GMP, cert-manager, Gateway API, and GKE custom
resources are schema-checked against pinned sources. This does not approve a
live controller installation. No Mindclade custom resource is approved by this
foundation. A future API must
have a structural schema, spec/status separation, conditions, status
subresource, idempotent reconcile contract, finalizer/owner-reference lifecycle,
conflict/backoff behavior, and envtest/connected evidence.

## Production promotion

Production promotion requires completed staging qualification on the identical
manifest digest and platform tuple, named operators, an approved change window,
capacity/quota evidence, SLO alert tests, restore/failover evidence, and an
executable rollback. The repository must record the rendered digest and empty
post-rollout drift result.

See `RUNBOOK.md` for operational steps and `MLOPS.md` for ML-specific gates.

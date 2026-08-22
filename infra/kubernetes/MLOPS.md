# Kubernetes MLOps contract

The Kubernetes foundation is currently inactive. This document defines the
evidence required before model-serving, data, preprocessing, training, or
evaluation workloads may raise the zero-workload quota.

## System map

| Plane | Kubernetes form | Authority | Required handoff |
|---|---|---|---|
| Online serving | Gateway + runtime host/model worker | Rust admission/runtime; Python tensor semantics | Immutable model/runtime descriptor and signed local authority |
| Durable inference | Job/JobSet admitted by Kueue | Go durable state; Rust staging; Python execution | Fenced execution ticket and prepared-input digest |
| Ingestion/curation | Jobs or bounded workers | Go orchestration; Rust byte path; Python scientific policy | Immutable source snapshot, schema, license, lineage, and output manifest |
| Preprocessing | CPU/high-memory Jobs/JobSets | Go DAG; Rust node execution; Python feature semantics | Reference snapshot, policy digest, resource class, and feature-bundle digest |
| Training | Kueue + JobSet | Go admission; Rust node agent; Python trainer | Resolved config, dataset/model/toolchain digests, topology, checkpoint policy |
| Evaluation | Isolated Jobs/JobSets | Independent evaluation authority | Candidate digest, hidden-set isolation, metric/safety thresholds, evidence record |

Kueue, JobSet, GPU, RDMA, and NCCL manifests remain blocked until their
controllers, CRDs, versions, hardware tuple, and rollback are qualified.

The static capacity classes are deliberately explicit:

- CPU ingestion and preprocessing use only `mindclade-batch-cpu`;
- H100 and B200 training use distinct namespaces, queues, node flavors, and
  quotas so capacity cannot cross hardware generations;
- `1g-packed` requests one accelerator and permits Kueue topology-aware packing;
- the H100 qualification profile requests one complete eight-GPU A3 node, selects
  only the Terraform-owned `on-demand` capacity label, and binds world size eight in
  the source qualification runner;
- the older two-replica B200 shape remains outside the reference qualification claim.

These are scheduling contracts, not performance evidence. Quota remains zero
until a qualification run establishes GPU memory headroom, NCCL/RDMA behavior,
checkpoint time, topology, fragmentation, and cost.

## Data and feature quality

Every pipeline boundary must publish a versioned contract covering:

- schema, type, nullability, range, categorical domain, units, and encoding;
- entity/primary key, event time, ingestion time, ordering, and late-data rule;
- freshness, completeness, uniqueness, referential integrity, and volume bounds;
- sensitive-field classification, lawful use, retention, deletion, and log policy;
- immutable source/dataset/reference/tool/config digests and lineage owner;
- deterministic split and feature logic, including random seeds and environment;
- point-in-time joins and label availability to prevent leakage;
- one shared transformation contract or parity tests to prevent train/serve skew.

Validation runs at ingestion, transformation, training input, model packaging,
and serving input. A failed contract quarantines the candidate artifact; it does
not silently coerce, drop, retrain, or promote.

The current workload YAML records this interface but does not yet implement the
signed envelope verifier, atomic output publisher, quarantine writer, or
safe-point checkpoint helper. Those missing executables and their evidence are
activation blockers; annotations are never treated as a completed quality gate.

## Model-serving contract

A release declares input/output schemas, compatibility version, error model,
privacy boundary, artifact format, warmup behavior, hardware/runtime tuple,
latency/throughput envelope, and fallback. Images and model artifacts are
immutable and independently digest-pinned.

Before traffic, qualify:

- startup, liveness, readiness, warmup, drain, cancellation, and maximum shutdown;
- CPU, RAM, GPU memory, pinned/shared memory, ephemeral disk, FD, process, queue,
  concurrency, request, token/atom, batch, and output bounds;
- cold start, steady-state p50/p95/p99, error rate, saturation, and load shedding;
- model/runtime compatibility, reference parity, reproducible packaging, and
  failure when a policy, route, revocation, model, or feature contract is stale;
- shadow/canary or blue-green promotion, rollback digest, and traffic guardrails.

Online serving must not synchronously wait for long preprocessing. Full
pipelines return a durable run identity and use separately admitted resource
stages.

## Pipeline and promotion

The minimum promotion chain is:

```text
validated immutable data
  -> deterministic preprocessing/features
  -> tracked training run and checkpoints
  -> independent evaluation and safety
  -> model/runtime bundle with SBOM and provenance
  -> non-production serving qualification
  -> reviewed registry promotion
  -> digest-pinned GitOps rollout
  -> monitored canary and rollback decision
```

Training records code, resolved configuration, dataset/reference/feature,
container/toolchain, topology, seed, checkpoint, and evaluation digests. A
registry transition is an explicit reviewed state change; a successful pipeline
exit alone cannot promote a model.

## Monitoring map

Telemetry must be bounded, redacted, tenant-safe, and owned:

| Layer | Minimum signals |
|---|---|
| Kubernetes/system | rollout, restarts, readiness, CPU/RAM/GPU/disk/FD, scheduling and quota, Kueue wait, JobSet completion |
| Serving | request/stream latency, throughput, faults, rejects, queue/batch size, load shed, warmup, model-slot and GPU saturation |
| Data | schema validity, missingness, ranges, category shift, freshness, volume, duplicates, quarantine |
| Prediction | output/score/confidence distribution, abstention/fallback, class or task balance, calibration where labels exist |
| Model quality | task metrics against delayed labels, regression slices, robustness, safety and fairness slices where applicable |
| Business/guardrail | task-specific outcome, cost per useful result, policy violations, human override and rollback rate |

Alert thresholds require a window, minimum traffic/sample count, burn-rate or
persistence condition, severity, owner, and runbook action. Do not page on raw
drift alone without enough evidence to act. Dashboards and alerts must use
bounded-cardinality identifiers; raw prompts, sequences, tensors, features,
labels, signed URLs, credentials, and model weights are not telemetry.

GKE Managed Service for Prometheus is the collection plane. Namespaced
`PodMonitoring` and `Rules` resources are co-owned with each observed platform
component; Prometheus Operator, `ServiceMonitor`, in-cluster Prometheus, and
Alertmanager are not installed. Cloud Monitoring/Terraform owns paging,
channels, and SLO burn alerts. A cluster feature flag alone is not scrape
evidence: target health, TLS, collector NetworkPolicy reachability, recording
rule evaluation, query results, and synthetic fire/recovery must all be proven
before capacity is enabled. JobSet capacity additionally requires a durable
condition/event-derived completion and failure signal; the upstream per-name
terminal counters are not treated as reliable windowed outcome events.

The reference-training families are fixed in
`infra/observability/training-metrics.json`: 73 maximum exporter series across
training progress, terminal run outcomes, checkpoint publication, and restore outcomes.
No run, checkpoint, artifact, model, dataset, tenant, or request identity is a label. The
named exporter is an external producer contract and is not implemented in this source tree;
therefore the H100 profile intentionally has recording rules but no training `PodMonitoring`.
The existing training alert values and availability profile are explicitly proposed,
unapproved, and disabled. Environment owners must supply any accepted SLO, cost, RPO, and RTO
values and retain synthetic fire-and-resolve evidence before activation.

## Feedback and retraining

Labels and feedback are immutable, access-controlled inputs with event time,
provenance, and delayed-label handling. Drift or quality alerts may open an
evaluation/retraining candidate, but may not trigger unattended promotion.
Retraining repeats data validation, leakage/skew checks, independent evaluation,
safety/security review, and canary gates. Rollback always targets the prior
qualified model/runtime/config tuple, not only a model weight file.

## Owner checklist

Before quota activation, name owners for the data contract, feature logic,
model, serving runtime, Kubernetes release, SLO/alerts, security/privacy review,
cost/capacity, incident response, and rollback decision. Attach the resulting
evidence to `PRODUCTION_READINESS.md` and the component maturity record.

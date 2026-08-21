# Data platform production readiness

Review date: 2026-08-21  
Owner: data-platform  
Scope: reusable source under `data/`; no cloud environment was mutated or
deployed by this review.

## Decision

**Source adoption: GO. Production promotion: NO-GO pending connected evidence.**

The reusable source boundary is implemented, typed, deterministic where
required, bounded at external inputs, and covered by Python, Go, and Bazel tests.
That is not enough to claim a production environment. GCP project topology,
service identities, CMEK and retention policy, network paths, provider quotas,
durable MLflow backend/artifact configuration, SLOs, load results, backup/restore,
and disaster-recovery drills were not supplied or inspected and remain blockers.

## Evidence classification

| Area | Classification | Evidence and decision |
| --- | --- | --- |
| Immutable identity and contracts | Observed / pass | Artifacts bind URI, provider generation/version, size, and SHA-256; dataset manifests pin sources, transforms, tokenizer/features, split, lineage, quality, uses, and evidence. |
| Ingestion and connectors | Observed / pass at source level | Canonical encoding, bounded records, replay-safe stages, deterministic rejection, pagination, HTTPS allowlisting, redirect denial, response limits, exact GCS generation and S3 version binding are tested. |
| Curation and bio-data policy | Observed / pass at source level | License, consent, provenance, deduplication, contamination, safety, and deterministic augmentation stages are explicit. No universal biological threshold was invented. |
| Quality and leakage | Observed / pass at source level | Aggregate-only reports, integrity, duplicate, group leakage, evaluation overlap, hidden-set, privacy, license, bias, drift, schema, and bio-safety checks are implemented. |
| Dataset lineage/publication | Observed / pass at source level | Acyclic lineage and publication transition validation exist. Durable publication authority remains the existing Go control plane. |
| PyTorch loading | Observed / pass locally | Stable ordering, rank+worker sharding, epoch seeding, resume, packing, bounded shuffle, experience replay, explicit collation, and custom pinned-memory behavior are tested. A real two-worker probe passed. |
| Reference data/tokenizers | Observed / pass at source level | Content-addressed snapshots/indexes and explicit protein, DNA/RNA, text, SMILES lexical, structure, and multimodal tokenizer contracts are tested. |
| MLflow lineage mirror | Observed / partial | The repository already provides an optional tracking/lineage exporter. This data layer does not make MLflow canonical. Connected durable SQL backend, object artifact store, HA, retention, backup, and restore evidence is unknown. |
| GCP deployment posture | Unknown / blocker | No deployed project, region, service account, workload identity, IAM condition, VPC path, bucket policy, BigQuery layout, KMS key, audit sink, quota, retention, or DR evidence was supplied. |
| Scale, cost, and SLOs | Unknown / blocker | Dataset volumes, throughput targets, freshness, concurrency, load-test percentiles, capacity headroom, unit cost, budgets, and alert thresholds were not supplied. |

`Observed` means directly present or executed in this repository. `Inferred`
claims are not used as promotion evidence. `Unknown` items require environment
owners to produce evidence; they are not silently treated as defaults.

## Verification performed

| Gate | Result |
| --- | --- |
| `nix --option eval-cache false develop .#ci --command tools/dev/bazelw test //data/... --config=ci` | 23/23 Bazel tests passed |
| `.venv/bin/python -m pytest data -q` | 44 tests passed |
| `.venv/bin/ruff check data` | passed |
| `.venv/bin/mypy data` | passed for 152 source files |
| `go test ./data/connectors/...` | all seven connector packages passed |
| PyTorch `DataLoader` with two workers | passed; unique worker seeds and complete, non-duplicated sample coverage |

The direct host Bazel invocation was also checked and correctly refused to run
without `MINDCLADE_CC_TOOLCHAIN_ROOT`; the pinned Nix CI shell is the qualifying
execution. Nix's optional evaluation cache was disabled for the final pass
because the host filesystem reported no space for its SQLite transaction; this
does not alter the pinned derivation or Bazel action. The first sandboxed multiprocessing probe could not access the host
shared-memory manager; the same bounded probe passed outside that sandbox.

## Promotion gates

Before production promotion, the environment owner must attach:

1. Exact project/region/storage/service inventory and data-classification map.
2. Per-workload identities, least-privilege IAM evidence, secret references,
   audit configuration, network policy, and encryption/key-rotation evidence.
3. Connected qualification for every enabled provider, including pagination,
   retry/backoff, quota exhaustion, corruption, revocation, and license drift.
4. Durable registry and MLflow topology evidence: transactional SQL metadata,
   object-backed artifacts, retention/GC, backup, restore, and HA behavior.
5. Representative-volume correctness and load tests, including distributed
   accelerator loading, failure injection, resume, and bounded-memory evidence.
6. Approved SLOs, alerts, runbooks, capacity/cost model, rollback procedure, and
   a successful restore/DR drill.
7. Signed dataset/reference release evidence and independent approval for
   restricted, hidden-evaluation, safety-sensitive, or regulated data.

Rollback for this source-only change is a code revert; no external resource or
dataset was created. Runtime rollback must select the last approved immutable
dataset/reference version rather than mutating a published version.

## Agentic data service decision

Do **not** add an authoritative agentic data service now. The existing split is
deliberate: Python owns scientific semantics, Rust owns byte-heavy execution,
and Go owns durable workflow/publication state. A new agent in that path would
add a second policy and state authority, weaken reproducibility, and enlarge the
credential and data-exfiltration surface without closing any readiness blocker.

An agentic capability may later be useful as a non-authoritative planner or
operator assistant. If pursued, it must use typed read-only discovery by
default, propose a deterministic plan, require policy-gate approval for writes,
execute only through existing signed tickets and Go transitions, pin every
input/tool/model version, emit tamper-evident audit evidence, and have no direct
credential, publication, hidden-set, or raw-sensitive-data bypass. Its failure
must never block ordinary deterministic pipelines.

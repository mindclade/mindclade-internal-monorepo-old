# Scale and capability milestones

## Milestone 0: qualified foundation

- Bazel/Nix toolchain contract works locally and remotely.
- `libs/go` and production adapters pass connected conformance.
- One ingestion source reaches immutable dataset publication.
- One prepared-input model runs through batch inference.
- Artifacts, lineage, audit, and release evidence are complete.

## Milestone 1: full biological pipeline

- PDB/UniProt/RNAcentral source snapshots and incremental cursors.
- Rust transfer/parser workers and local reference cache.
- Python curation plus MSA/template/ligand/featurization workflow.
- NovaFold-style full-pipeline prediction with durable run state.
- Separate CPU/high-memory and GPU queues.

## Milestone 2: production training and serving

- Native/TorchTitan execution adapters and verified distributed checkpoints.
- Independent evaluation/safety release gates.
- Rust runtime gateway/host with signed authority and Python tensor batching.
- Canary/rollback and signature-specific TileLang qualification.

## Milestone 3: fleet scale

- Multicluster scheduling, topology-aware queues, remote execution, capacity
  forecasting, and automated recovery qualification.
- Split control-plane modules only where decomposition triggers are met.
- Regional route/reference/artifact policy and bounded outage operation.

## Milestone 4: frontier scale

- Large multinode training, disaggregated RL/simulation, advanced model/data
  governance, confidential/attested runtime where required, and continuous
  evidence-driven promotion.

Every milestone has explicit numerical, reliability, security, cost, and
recovery gates; “number of services” is not a milestone.

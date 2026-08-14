# Mindclade engineering architecture

This documentation records the accepted system architecture, implementation
boundaries, operational behavior, security model, qualification expectations,
and target-state repository for the Mindclade platform.

## Start here

1. [Canonical system design reference](architecture/system-design-reference.md)
2. [System-design traceability](architecture/system-design-traceability.md)
3. [System overview](architecture/system-overview.md)
4. [Architecture decision register](design/decision-register.md)
5. [Language boundaries](architecture/language-boundaries.md)
6. [Dependency rules](architecture/dependency-rules.md)
7. [Go foundation architecture](architecture/go-foundation.md)
8. [Rust runtime foundation](guides/rust-runtime-foundation.md)

## End-to-end systems

- [Go control plane](architecture/control-plane.md)
- [Runtime authority and unified stages](architecture/runtime-authority-and-stage-execution.md)
- [Runtime data plane](architecture/runtime-data-plane.md)
- [Data ingestion](architecture/data-ingestion.md)
- [Dataset publication](architecture/dataset-publication.md)
- [Scientific preprocessing](architecture/preprocessing.md)
- [MSA and template search](architecture/msa-and-template-search.md)
- [Reference data and release evidence](architecture/reference-data-and-release-evidence.md)
- [Serving](architecture/serving.md)
- [Training](architecture/training.md)
- [Distributed training](architecture/distributed-training.md)
- [Checkpointing](architecture/checkpointing.md)
- [Evaluation](architecture/evaluation.md)
- [Artifact lifecycle](architecture/artifact-lifecycle.md)
- [Release evidence](architecture/release-evidence.md)
- [Build and toolchains](architecture/build-and-toolchains.md)
- [Eighteen optimization implementation record](architecture/optimization-18-implementation.md)

## Implementation status

The repository deliberately distinguishes source implementation from
qualification and production promotion.

Implemented foundations include the Go mechanism/control-plane substrate, the
adopted and deepened Rust runtime/node foundation and gateway/host cores,
runtime authority and routing contracts, artifact/reference/evidence models,
deterministic Python configuration resolution, preprocessing contracts/DAG
and provenance seams, cross-language fixtures, and architecture/maturity/
dependency enforcement.

Provider-backed networking/storage, full scientific algorithms, complete model
families, scale qualification, production cloud deployment, and other leaves
remain subject to their local maturity/readiness status and connected
qualification. A path existing never means it is production-ready.

See root `REPOSITORY_STATUS.md`, `SCAFFOLD_STATUS.md`, `VALIDATION.md`,
`QUALIFICATION.md`, `components.toml`, and `maturity.toml`.

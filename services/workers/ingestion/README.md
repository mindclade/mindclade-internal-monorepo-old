# Ingestion worker

**Language:** Rust  
**Status in this archive:** adapter implemented; source/provider composition and qualification pending.

## Role

Executes broker/job-driven source download, resume, decompression, bounded parsing, raw-artifact commit, and stage status around the reusable ingestion contracts.

## Boundary

The worker is a deployable adapter. Reusable semantics live in the owning
`data/`, `preprocessing/`, `models/`, `serving/`, `training/`, or `evaluation/`
package. It consumes immutable inputs and a signed execution ticket, reports
versioned status, commits outputs atomically by digest/manifest, and honors
cancellation, deadline, fencing, retry, and drain.

It does not own:

- scientific curation and dataset policy;
- source/workflow authority;

## Required operational behavior

- validate ticket, bundle, artifact scope, attempt, and fencing before work;
- reserve bounded CPU/RAM/disk/GPU/process/output resources;
- make stage retries explicit and idempotent;
- keep control RPCs small and place bulk data in files/shared memory/object refs;
- preserve deterministic seeds/config/policy/reference/tool provenance;
- reject late output/status commits after cancellation or claim loss;
- emit bounded diagnostics and release resources during drain.

## Limitations

The checked-in Rust adapter validates configuration, uses the shared ticketed worker runtime,
tracks lifecycle, and delegates to an injected ingestion engine under bounded execution. The
binary exits with `EX_CONFIG` until deployment supplies a source/provider engine. Connected source,
resume/decompression/parser, artifact-publication, load, and failure qualification remain required.

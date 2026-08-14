# Data curation

**Status:** implemented deterministic model-neutral curation core; scientific
source-specific policies remain independently qualified.

## Ownership

Python owns scientific curation semantics. Go owns durable ingestion workflow,
source cursors, retries, and publication policy. Rust owns high-throughput byte
transfer, bounded parsing, node-local caches, and external-process execution.

## Implemented core

`pipeline.py` provides immutable `CuratedRecord` values and a bounded,
deterministic `CurationPipeline` with:

- canonical record digests;
- bounded keys, payloads, and metadata;
- deterministic transform ordering;
- explicit drop behavior;
- duplicate coalescing and conflicting-duplicate rejection;
- deterministic sorted output and manifest SHA-256.

Package-local tests cover deterministic output, drops, and conflicting duplicate
behavior. PDB/UniProt/RNAcentral-specific normalization, licensing, safety, and
quality policy remains in their owning curation stages rather than this generic
pipeline mechanism.

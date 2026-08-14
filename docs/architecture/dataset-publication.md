# Dataset publication

Dataset publication converts completed curated/model-ready artifacts into an
immutable, discoverable version that training, evaluation, and release systems
can depend on safely.

## Dataset version manifest

A published version records:

- dataset ID, version, owner, intended uses, prohibited uses;
- source snapshot IDs and artifact digests;
- canonicalization, curation, tokenizer, and featurization versions;
- shard/index manifests and deterministic ordering/shuffle policy;
- schemas, statistics, quality reports, license/consent evidence;
- contamination, leakage, privacy, and safety assessments;
- lineage graph digest and build/toolchain provenance;
- publication time, approval records, and rollback/supersession state.

## Publication state machine

```text
draft -> validating -> qualified -> published -> deprecated -> retired
                     \-> rejected
```

Content never mutates after qualification. A correction creates a new version
and explicit supersession relation.

## Gates

- every referenced artifact exists and verifies;
- manifests and schemas are compatible;
- counts, sizes, checksums, and statistics agree;
- licenses/consent and retention policy are satisfied;
- hidden evaluation and holdout leakage checks pass;
- quality and biological-risk thresholds pass;
- lineage is complete enough to reproduce the build;
- representative loader smoke and deterministic-resume tests pass.

## Control-plane transaction

Publication updates registry state, audit, lineage links, and the dataset-
published outbox event in one transaction. Consumers use immutable dataset IDs
and versions, never a mutable filesystem path.

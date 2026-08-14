# MSA and template search

This pipeline serves NovaFold-like structure models and any future model that
requires evolutionary and structural context.

## Durable DAG

```text
validate/canonicalize entities
  -> deduplicate identical chains
  -> per-entity feature-cache lookup
  -> protein MSA search and/or RNA MSA search
  -> profile construction
  -> template search against immutable snapshot
  -> cutoff, filtering, deduplication, structure retrieval
  -> paired-MSA construction for complexes
  -> ligand/CCD preparation in parallel
  -> model-specific featurization
  -> PreprocessedInputBundle commit
```

Template search may depend on the MSA-derived profile, so it is modeled as an
explicit dependent stage rather than an unrelated parallel call.

## Reference database snapshots

A snapshot records database type, upstream versions, release cutoff, shard and
index manifests, content digests, search-tool compatibility, format version,
size, promotion state, and retention policy. Rust node agents atomically stage
verified read-only snapshots to local NVMe. Python workers refer to immutable
snapshot IDs; they never infer “latest.”

## Cache keys

### Per-entity MSA

```text
normalized sequence digest
+ entity type
+ search protocol and parameters digest
+ search tool/version
+ reference snapshot digests
```

### Template hits and selection

```text
sequence/profile digest
+ template snapshot digest
+ maximum template date
+ search/filter/selection policy digests
+ tool/version
```

### Paired MSA

```text
ordered chain digests
+ per-chain MSA digests
+ pairing/cropping policy digests
```

### Final feature bundle

```text
canonical complex digest
+ MSA/template/ligand artifact digests
+ feature schema and model input contract versions
+ featurization policy digest
```

## Failure and retry

Search subprocess failures capture command identity, bounded stdout/stderr,
exit status, resource usage, database snapshot, and work directory diagnostics.
A timed-out or preempted search can retry from immutable inputs. Scientific
validation failures are normally terminal until policy/input changes.

## GPU boundary

No GPU is reserved while search or template work is pending. GPU admission
occurs only after the complete preprocessed bundle verifies and the model
resource estimator accepts its token/atom/MSA/template/sample envelope.

# Runbook: scientific preprocessing stalled

## Trigger

MSA generation, profile construction, template search, ligand preparation,
paired-MSA construction, or model featurization exceeds stage limits or stops
progressing.

## Triage

- Record run/stage/attempt, entity digest, search policy digest, tool version,
  reference-database snapshot, cache key, worker/node, lease/ticket, CPU/RAM,
  disk I/O, subprocess state, and last progress marker.
- Separate Go workflow/queue failure from Rust tool/cache/resource failure and
  Python scientific-policy failure.
- Verify that no GPU slot is reserved while the preprocessing stage is pending.

## Recovery

- Reference cache missing/corrupt: follow `reference-cache-corruption.md`.
- External tool hung: Rust supervisor terminates the process tree at deadline,
  captures bounded diagnostics, and releases resources before retry.
- Resource estimate too low: retry under an explicitly larger class; do not
  remove memory/output limits.
- Scientific parsing/filtering defect: preserve raw search artifacts and rerun a
  versioned Python stage without repeating expensive search where cache keys
  remain valid.
- Repeated deterministic failure: quarantine the input/stage and mark the run
  with an explicit terminal diagnostic rather than infinite retry.

## Exit criteria

The stage produces a verified immutable output or explicit terminal result,
cache/provenance keys are complete, retries are bounded, and downstream GPU
inference starts only after the input bundle commits.

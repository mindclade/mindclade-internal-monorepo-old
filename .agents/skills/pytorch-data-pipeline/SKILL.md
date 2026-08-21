---
name: pytorch-data-pipeline
description: Build or debug PyTorch Dataset, IterableDataset, DataLoader, sampler, transform, collation, worker, prefetch, and distributed data-input code. Use for incorrect batches, duplicate or missing samples, worker crashes, reproducibility problems, CPU bottlenecks, or input-pipeline design. Do not use for model architecture or end-to-end optimizer logic.
license: MIT
compatibility: Designed for Codex and other Agent Skills-compatible clients. Project commands require Python and the repository's installed PyTorch version.
metadata:
  version: "1.0.0"
  domain: "pytorch"
---
# Objective

Create a data pipeline with explicit sample and batch contracts, correct sharding, reproducible randomness where required, and measured throughput without hiding data-quality errors.

# Workflow

1. Inspect real samples and every transformation before changing loader settings. Record the sample structure, shapes, dtypes, ranges, labels, masks, and variable-length behavior.
2. Decide whether the source is map-style or iterable. Preserve stable indexing for map-style data and implement worker and rank sharding explicitly for iterable sources.
3. Make the collation contract explicit. Handle variable shapes with padding, packing, bucketing, nested tensors, or a custom collate function rather than relying on accidental Python lists.
4. Validate the pipeline with `num_workers=0` first so exceptions remain local and readable.
5. Add worker processes only after correctness. Ensure executable entry points use the platform-appropriate main guard and that dataset state is safe to create or copy in workers.
6. Configure randomness deliberately. Seed the main generator and derive per-worker seeds when repeatability matters; do not force deterministic augmentation when the training design expects independent randomness.
7. For distributed map-style training, use an appropriate distributed sampler and call `set_epoch(epoch)` when shuffling. For iterable data, shard by both rank and worker.
8. Test sample coverage, duplicates, ordering assumptions, the final partial batch, batch size one, corrupt records, and empty partitions.
9. Measure data-only throughput and host-to-device overlap separately from model compute before changing `pin_memory`, `persistent_workers`, prefetching, caching, or worker count.
10. Report the sample and batch contracts, sharding rule, seed rule, worker configuration, coverage tests, and measured throughput.

# Engineering rules

- Do not return accelerator tensors from DataLoader workers. Transfer batches in the training process.
- Do not swallow corrupt-sample exceptions by silently skipping records unless the data policy explicitly defines and counts skips.
- Keep transforms picklable when spawn-based multiprocessing is possible.
- Avoid opening one shared non-fork-safe client or file handle before worker creation. Initialize worker-local resources deliberately.
- Use `pin_memory` only when it supports a measured asynchronous transfer path; it is not a universal speed switch.
- Use `persistent_workers` only when repeated epochs amortize worker startup and dataset state is safe to persist.
- Preserve exact sample counts and denominator semantics when `drop_last`, sharding, or filtering is used.

# Definition of done

- Sample and batch structures are documented and checked.
- Coverage tests detect unintended duplicates or omissions.
- The pipeline works with zero workers and with the intended worker count.
- Distributed sharding is disjoint and complete to the degree the sampling policy promises.
- Throughput changes are supported by comparable measurements rather than intuition.

Run `scripts/dataloader_worker_probe.py` to verify that basic worker launch and seeding work in the current Python environment. Read [the DataLoader checklist](references/dataloader-checklist.md) for project-specific tests.

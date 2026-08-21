# Data pipeline checklist

## Sample contract

For each field, record:

- semantic meaning;
- Python or tensor type;
- shape and variable dimensions;
- dtype and numerical range;
- missing-value or padding convention;
- ownership of decoding and normalization;
- whether randomness is expected.

## Map-style datasets

Test that:

- `__len__` matches the addressable index set;
- repeated access to the same index is deterministic unless augmentation is intentionally random;
- invalid indices fail clearly;
- split construction does not leak examples across train, validation, and test;
- a sampler, not the dataset, controls ordering when practical.

## Iterable datasets

Each rank and worker must receive a deliberate shard. Use rank information and `get_worker_info()` to avoid every worker iterating the full source. Define whether the source is finite, resumable, and exactly-once or at-least-once.

## Multiprocessing diagnosis

Use this reduction order:

1. `num_workers=0`;
2. a tiny deterministic dataset;
3. no custom collate function;
4. no augmentation;
5. one worker;
6. the intended worker count;
7. persistent workers and prefetch settings last.

On spawn-based platforms, keep dataset classes and worker functions importable at module scope and protect launch code with `if __name__ == "__main__":`.

## Reproducibility

Seed policy should cover the main process, sampler or DataLoader generator, and worker-local Python, NumPy, and PyTorch randomness when those libraries are used. Reproducibility across releases or devices is not guaranteed merely by setting a seed.

## Performance measurement

Measure at least:

- samples or batches per second with no model work;
- batch construction latency distribution;
- host-to-device copy time if an accelerator is used;
- queue starvation or idle accelerator time;
- memory growth across epochs with persistent workers.

Change one variable at a time. Worker count can be limited by storage, decompression, Python overhead, CPU threads, shared memory, or remote service limits.

Official references:

- Data loading: https://docs.pytorch.org/docs/stable/data.html
- Multiprocessing practices: https://docs.pytorch.org/docs/stable/notes/multiprocessing.html
- Reproducibility: https://docs.pytorch.org/docs/stable/notes/randomness.html
- Distributed sampler: https://docs.pytorch.org/docs/stable/data.html#torch.utils.data.distributed.DistributedSampler

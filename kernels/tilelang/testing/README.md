# TileLang correctness and performance harness

- **Status:** Implemented reusable test records; connected-device runs remain outstanding.

Fixed dtype policies cover FP32, FP16, BF16, E4M3, and E5M2 without silently
loosening tolerances. `parity_report` records maximum absolute/relative error,
mismatch count, NaN/Inf agreement, and case size. Compile checks capture source
and diagnostics; golden digests detect unintended codegen changes.

`benchmark_callable` requires prior correctness and an explicit synchronization
callback. It performs warmup, synchronizes before and after each sample, and
returns the complete sample set plus median/MAD. Benchmark records bind request,
implementation, and environment digests.

Production qualification additionally requires adversarial shapes, tails,
misalignment, non-contiguous rejection, all-masked/empty cases as applicable,
determinism, gradient parity where required, sanitizers, cold/warm compile
behavior, and baseline comparisons on the exact target.

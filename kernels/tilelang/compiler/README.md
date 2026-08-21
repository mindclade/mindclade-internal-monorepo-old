# TileLang compiler support

- **Status:** Implemented host-side contracts and diagnostics; device lowering requires a pinned accelerator environment.

This package owns target-independent legality and compilation records:

- thread/layout coverage and vector-alignment checks;
- shared-memory swizzle and TMA descriptor validation;
- pipeline stage/resource rejection;
- WGMMA and warp-specialization capability checks;
- content-addressed generated-source capture and bounded compile caching;
- redacted, stable compiler diagnostics.

`compile_kernel` records source and environment digests rather than treating a
successful Python import as compilation evidence. Target-specific options are
validated before invoking a backend. Generated source inspection is a required
qualification input, especially for expected tensor-core, async-copy, and
synchronization instructions.

These helpers do not silently downgrade TMA, warp specialization, or WGMMA.
Unsupported mappings are rejected so the registry can select a separately
identified portable schedule or the PyTorch fallback.

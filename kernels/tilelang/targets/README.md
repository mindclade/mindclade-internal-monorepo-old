# TileLang accelerator targets

- **Status:** Implemented static capability models; runtime discovery still verifies the actual device.

The registry describes CUDA `sm_90`, `sm_100`, `sm_120` and AMD CDNA
`gfx90a`, `gfx942`, `gfx950`. Each target records its TileLang target string,
supported dtypes, shared-memory/thread limits, warp size, and availability of
async copy, tensor cores, TMA, WGMMA, TMEM, and warp specialization.

Schedules express requirements through `TargetRequirement`. Rejection reasons
are stable (`dtype`, `shared_memory`, `threads`, `async_copy`, `tma`, and so on)
and are recorded by dispatch/tuning rather than hidden by a fallback inside a
kernel factory.

Static models are allowlists, not runtime assertions. Driver/runtime identity,
device UUID/model, and actual architecture remain part of qualification evidence.

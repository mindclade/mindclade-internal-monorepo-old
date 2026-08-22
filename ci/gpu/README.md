# Connected GPU validation

- **Status:** Implemented validation matrix; production promotion is disabled.
- **Owner:** `biology-ml`

`targets.yaml` models CUDA `sm_90`, `sm_100`, `sm_120` and ROCm `gfx90a`,
`gfx942`, `gfx950`. No entry is currently promotion-eligible. The `sm_90` job
is a qualification-candidate validation lane; it cannot publish a production
manifest until compiled-artifact loading, sanitizer execution, evidence
production, and trusted signing are connected.

`pipeline.py` validates the matrix, resolves exactly one architecture, and
executes explicit Bazel labels through the pinned Nix/Bazel wrappers without a
shell. It carries the future memcheck, racecheck, initcheck, and synccheck
requirements as metadata; this driver does not claim to execute them.

The H100 validation targets cover:

- exact TileLang/TVM-FFI runtime validation;
- source compilation and generated-instruction inspection;
- tail, causal, activation, adversarial, determinism, and reference parity;
- explicit training fallback plus reference-gradient coverage;
- synchronized warmup and 50-sample latency distributions;
- 5% median speedup, 5% relative-MAD, p95, and peak-memory gates.

Run matrix validation without executing a job:

```bash
tools/dev/nixw develop .#ci-bazel --command tools/dev/bazelw run \
  //ci/gpu:pipeline --config=ci -- --architecture sm_90
```

Connected execution additionally requires `--execute`, the expected target and
architecture, explicit compiler/driver identities, and an OCI
`MINDCLADE_RUNTIME_IMAGE_DIGEST`. Local CPU runs skip connected tests and do not
create qualification evidence. Sanitizer logs, raw measurements, attestations,
and a trusted manifest signer remain promotion blockers.

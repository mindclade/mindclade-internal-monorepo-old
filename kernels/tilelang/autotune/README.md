# Offline TileLang autotuning

- **Status:** Implemented bounded search and evidence records; no tuned winner is checked in yet.

Autotuning is offline and correctness-first. `SearchSpace` has an explicit
maximum candidate count, `TuningBudget` limits compile and benchmark work, and
every candidate receives a terminal status with a stable failure digest.
Illegal schedules are rejected before compilation; compilable schedules must
pass reference parity before timing.

Latency is summarized with median and median absolute deviation. The tuning
database binds results to request, source, schedule, target, device,
driver/runtime, TileLang, compiler, and repository identities. A result from a
different environment is a cache miss, not a near match.

Tuning output is machine-readable evidence. It never mutates dispatch defaults
or publishes a qualification record; review and promotion are separate steps.

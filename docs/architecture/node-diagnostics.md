# Node diagnostics and resource accounting

The Rust node plane exposes a bounded, redacted diagnostics bundle and a hierarchical resource tree. Diagnostics contain identities, runtime version, active ticket IDs, recent process-exit metadata, cache/spool byte counts, safe attributes, and the `BudgetTreeSnapshot`. They never contain credentials, private signing keys, raw model weights, or raw customer/scientific inputs.

Every substantial local allocation is explainable through a budget account. The observable tree reports limits, reservations, waiters, rejections, and corruption state. OS/GPU free-memory observations are supplementary; they never override Mindclade reservations.

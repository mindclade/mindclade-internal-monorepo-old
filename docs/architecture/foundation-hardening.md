# Foundation hardening and freeze criteria

The architecture is frozen after the following gates are satisfied: affected-only presubmit selection, two-phase artifact GC, pinned Rust lock/toolchain qualification, Rust supply-chain policy, runtime compatibility qualification, failure-injection invariants, performance budgets, bounded node diagnostics/resource trees, canonical workload envelopes, and four golden end-to-end vertical slices.

The repository should not add a new generic Go package or broad Rust subsystem to solve a problem already expressible through these mechanisms. New design work is driven by measured workload requirements.

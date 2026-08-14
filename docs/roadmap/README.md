# Architecture evolution roadmap

This roadmap records when target-state boundaries should become live or split
into independent deployables. It is not a file-count plan.

Materialize a capability as a complete vertical slice with owner, contract,
implementation, Bazel target, tests, documentation, operational limits, and
qualification evidence. Do not create empty package symmetry.

Start with the modular Go control plane, Rust runtime/data-plane composition
roots, Python scientific/model packages, and the minimum infrastructure needed
for one end-to-end ingestion, preprocessing, training, evaluation, and serving
path. Expand only from measured bottlenecks and product requirements.

See `decomposition-triggers.md` and `scale-milestones.md`.

# Foundation freeze qualification

`python tools/qualification/foundation_freeze.py` runs the offline architecture, compatibility, supply-chain-policy, failure-matrix, performance-policy, cross-language, vertical-slice, workload, and artifact-GC gates.

The connected release invocation adds `--connected`, requires the pinned Rust 1.97.1 toolchain and committed Cargo-generated lock, cargo-deny, real Rust formatting/test/Clippy/docs/Miri/fuzz lanes, provider-backed failure injection and measured performance results. Architecture is considered frozen only after this connected evidence is retained with the release.

# Rust Foundation SLO

`libs/rust` is a tier-1 runtime foundation. Production releases require a fully locked Rust workspace, green format/test/Clippy/supply-chain lanes, and no regression in the published Rust performance budgets. No individual library has an availability SLO independent of its consuming service; its objective is release correctness and bounded runtime behavior.

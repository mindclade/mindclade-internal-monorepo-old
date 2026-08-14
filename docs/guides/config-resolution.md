# Resolved configuration guide

`libs/python/config` is the canonical compositional resolver for numerical and
scientific configurations. Inputs are layered in explicit order:

```text
base
+ model/task recipe
+ data profile
+ hardware profile
+ parallelism profile
+ precision profile
+ kernel/runtime profile
+ environment
+ explicit overrides
= ResolvedConfig
```

The resolver rejects silent type replacement, applies deterministic deep merge
semantics, records every source file with its own digest, records explicit
overrides, serializes the resolved value as canonical JSON and emits a SHA-256
digest. The resolved document—not the implicit list of TOML files—is persisted
in run/checkpoint/evaluation/release provenance.

Domain schemas still own semantic validation. The resolver owns composition and
fingerprinting, not training or serving policy.

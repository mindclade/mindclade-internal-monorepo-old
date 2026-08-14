# `libs/go` admission policy

`libs/go` is frozen as a mechanism layer. A new top-level library requires an architecture review and all of:

1. Domain-neutral semantics.
2. At least two independent production consumers.
3. No ownership of run/model/dataset/tenant/ingestion/training business policy.
4. A provider-neutral contract when external systems are involved.
5. A conformance suite.
6. A valid Layer 0–4 dependency placement.

Business policy belongs under `control/`; executable composition belongs under `services/`.
`ADMISSION.toml` is checked in presubmit so accidental new dumping-ground roots fail closed.

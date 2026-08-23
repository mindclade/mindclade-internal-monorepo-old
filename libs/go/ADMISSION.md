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

## What presubmit actually enforces

Three of the six criteria have a gate behind them, and only partially. It is
worth knowing which:

| Criterion | Enforcement |
|---|---|
| 1, 3 — domain-neutral, no business policy | name only: `ADMISSION.toml` `forbidden_names`, applied at every depth by `tools/analysis/check_libs_go_admission.py`. The semantics behind an allowed name are review-only |
| 2 — two independent production consumers | **review only.** Nothing counts importers |
| 4 — provider-neutral contract | review only |
| 5 — conformance suite | review only; `UNCONSUMED.toml` will report a suite that nothing runs, but not a mechanism that has no suite |
| 6 — Layer 0–4 placement | `tools/analysis/check_go_layers.py` against `LAYERS.md` |

The unenforced half is not decorative. `CONSUMPTION.md` claimed the Kubernetes
tree was "admitted rather than merely linked" while `kubernetes/client` and
`kubernetes/controller` each had exactly one in-repo importer and six of the
tree's eleven packages were reachable only from a single integration test. Say
which importers satisfy criterion 2 in the review, by name, because no gate
will.

`tools/analysis/check_foundation_consumption.py` is the closest thing to an
admission gate that exists: it fails a `libs/go` package that nothing imports,
which catches criterion 2 at zero consumers but not at one.

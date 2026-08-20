# Dependency rules

## Bazel layer authority

`tools/build/bazel/layers.bzl` is the machine-readable source of truth for
repository-wide Bazel package membership. The root `BUILD.bazel` materializes
its entries as named `package_group` targets, so BUILD visibility declarations
may reuse the same vocabulary enforced in CI.

| Layer | Top-level packages | May depend on |
| --- | --- | --- |
| foundation | `configs`, `libs`, `protocols`, `sdk` | foundation and external dependencies |
| offline | `data`, `evaluation`, `kernels`, `models`, `preprocessing`, `training` | foundation and narrower offline contracts |
| serving | `apps`, `control`, `services`, `serving` | foundation, published model/data contracts, and serving internals |
| research | `research` | production packages and research |
| platform | `architecture`, `ci`, `docs`, `examples`, `infra`, `qualification`, `security`, `tests`, `tools` | the packages needed to build, validate, qualify, and deploy the repository |

The file also defines smaller `boundary_*` groups for rules that are more
precise than an entire layer. A new top-level code package is incomplete until
it is classified there and assigned in both `OWNERS.toml` and
`.github/CODEOWNERS`.

## Top-level flow

```text
protocols
    -> generated bindings and foundational libraries
    -> data / preprocessing / kernels / models
    -> training / serving / evaluation
    -> services
    -> apps through SDKs and contracts only
```

Allowed supporting directions:

```text
research -> production packages       allowed
production packages -> research       forbidden
infra -> release manifests/targets    allowed
source packages -> infra              forbidden
apps -> generated SDKs/contracts      allowed
apps -> services                      forbidden
model family -> sibling family        forbidden
```

The Bazel graph additionally makes these direct boundary crossings explicit:

```text
serving -> training                    forbidden
serving -> research                    forbidden
outside research -> research/experiments
                                        forbidden
apps -> services                       forbidden
source -> infra                        forbidden
```

The broader `production -> research` rule subsumes the serving/research case.
Research may depend on production so experiments can evaluate real components;
production must consume only promoted contracts and implementations.

## Go layers

```text
Layer 0  clock, faults, identifiers
Layer 1  audit, auth, idempotency, messaging contracts, pagination,
         request metadata, resource versions, signing, narrow storage contracts
Layer 2  config, observability, retry, servicekit, durable coordination loops
Layer 3  PostgreSQL/GCS/Redis/Kubernetes/provider adapters
Layer 4  HTTP, Connect, and gRPC transports
Layer 5  control domains, services, operators, and workers outside libs/go
```

Lower layers never import higher layers. `libs/go` never imports `control/`,
`services/`, `data/`, models, or executable packages.

## Service law

A service may consume protocols, libraries, control/domain engines,
orchestration, and specialized model/data packages. Reusable code must not
import a deployable. A process entry point should contain configuration,
provider construction, registration, and exit policy—not domain algorithms.

## Test placement

Unit and component tests are colocated with the package. Top-level `tests/`
contains only cross-package, cross-process, cross-language, device, numerical,
performance, resilience, scale, and security qualification.

## Provider law

Domain packages depend on narrow interfaces. Provider SDKs are created in
composition roots or provider adapter packages. A generic abstraction is added
only after two real consumers share a stable mechanism and conformance suite.

## Enforcement

- root Bazel package groups declared from `tools/build/bazel/layers.bzl`;
- `tools/analysis/check_bazel_layers.py`, which checks direct internal
  `rule-input` edges from `bazel query //... --output=xml --noimplicit_deps`;
- `tools/analysis/check_go_layers.py`;
- promoted-foundation and paved-road checks;
- `servicekit/production` role capability validation;
- cross-language golden tests;
- no-host-tool and Bzlmod/Nix toolchain checks;
- repository checks forbidding `common`, `shared`, `helpers`, or `utils`
  dumping grounds.

The query check is language-independent: Go, Rust, Python, generated targets,
and filegroups are governed by the same graph. Source-level checks remain in
place for language semantics that BUILD metadata cannot express.

Exceptions require an accepted ADR and an exact source-target entry in
`BAZEL_LAYER_EXCEPTIONS`. Values use `ADR-NNNN: rationale`; wildcards are not
accepted, and CI rejects an exception after its edge disappears. A permanent
new dependency direction is a policy change, not an exception, and requires an
ADR plus updates to the central groups, documentation, and ownership routes.

Run the same gates locally with:

```bash
python3 tools/analysis/check_bazel_layers.py
tools/dev/bazelw build //... --nobuild --config=ci
```

## Deprecated Rust compatibility paths

The uploaded Rust foundation historically exposed `clock`, `retry`,
`resource_version`, `observability`, `artifact_manifest`, `byte_spec`, and
`python_bindings` as standalone crates. The canonical architecture now
consolidates those implementations into `runtime_core`, `telemetry`,
`manifests`, `bytes_io`, and `python_bridge`.

The old crate names remain temporarily as **deprecated re-export facades only**.
Production crates must not depend on them. `tools/analysis/check_code_docs_alignment.py`
enforces both properties: compatibility crates own no duplicate source modules,
and active Rust code imports only the canonical implementations.

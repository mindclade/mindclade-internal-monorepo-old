# Dependency rules

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

- Bazel visibility and package groups;
- `tools/analysis/check_go_layers.py`;
- promoted-foundation and paved-road checks;
- `servicekit/production` role capability validation;
- cross-language golden tests;
- no-host-tool and Bzlmod/Nix toolchain checks;
- repository checks forbidding `common`, `shared`, `helpers`, or `utils`
  dumping grounds.

Exceptions require an accepted ADR and an explicit boundary update.

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

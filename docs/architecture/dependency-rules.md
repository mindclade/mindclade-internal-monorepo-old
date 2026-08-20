# Dependency rules

## Bazel layer authority

`tools/build/bazel/layers.bzl` is the machine-readable source of truth for
repository-wide Bazel package membership. The root `BUILD.bazel` materializes
its entries as named `package_group` targets, so BUILD visibility declarations
may reuse the same vocabulary enforced in CI.

| Source layer | Top-level packages | Allowed internal destinations |
| --- | --- | --- |
| foundation | `configs`, `libs`, `protocols`, `sdk` | foundation plus root/build/test support |
| offline | `data`, `evaluation`, `kernels`, `models`, `preprocessing` | foundation, offline, and root/build/test/release support |
| training | `training` | foundation, offline, training, and support |
| runtime | `control`, `serving` | foundation, offline, runtime, and support |
| services | `services` | foundation, offline, runtime, services, and support |
| apps | `apps` | foundation, apps, and support |
| research | `research` | every production layer, research, and support |
| platform/support | architecture, CI, docs, examples, infra, qualification, security, and narrowly classified tools | every layer |

Root metadata, `tools/build`, `tests`, and `tools/release` are separate support
domains so production access to a test runner or release helper does not imply
access to arbitrary CI, infrastructure, or developer tooling. Every internal
Bazel package must match exactly one domain. An unclassified or multiply
classified package fails immediately, as does a matrix key or destination that
does not name a declared domain. A new top-level code package is incomplete
until it is classified here and assigned in both `OWNERS.toml` and
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

The allow matrix is fail-closed: a direction not listed is forbidden even if no
past incident named it. Research may depend on production so experiments can
evaluate real components; production must consume only promoted contracts and
implementations.

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

Exceptions require an exact live source-target entry in
`BAZEL_LAYER_EXCEPTIONS` with `owner`, `adr`, `reason`, and `expires_on` fields.
The owner must be declared in `OWNERS.toml`, the ADR must exist and be accepted,
the rationale must be non-empty, and expiry may be at most 90 days from the
validation date. Wildcards, expired entries, and stale entries whose edge no
longer exists fail CI. A permanent new dependency direction is a policy change,
not an exception, and requires an ADR plus updates to the central matrix,
documentation, and ownership routes.

Run the same gates locally with:

```bash
tools/dev/nixw develop .#ci-bazel --command python3 tools/analysis/check_bazel_layers.py
tools/dev/nixw develop .#ci-bazel --command \
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

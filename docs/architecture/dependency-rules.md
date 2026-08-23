# Dependency rules

## Bazel layer authority

`tools/build/bazel/layers.bzl` is the machine-readable source of truth for
repository-wide Bazel package membership *and* for every permitted dependency
direction. The root `BUILD.bazel` materializes its entries as named
`package_group` targets, so BUILD visibility declarations reuse the same
vocabulary enforced in CI. The tables below render that file; where a table and
the file disagree, the file wins, and `tools/analysis/check_bazel_layers.py` is
what actually fails the build.

`BAZEL_LAYERS` declares **thirteen** domains. Every internal Bazel package must
match exactly one. An unclassified or multiply classified package fails
immediately, as does a matrix key or destination that does not name a declared
domain. A new top-level code package is incomplete until it is classified here
and assigned in both `OWNERS.toml` and `.github/CODEOWNERS`.

| Domain | Package patterns |
| --- | --- |
| `foundation` | `//configs/...`, `//libs/...`, `//protocols/...`, `//sdk/...` |
| `offline` | `//data/...`, `//evaluation/...`, `//kernels/...`, `//models/...`, `//preprocessing/...` |
| `training` | `//training/...` |
| `runtime` | `//control/...`, `//serving/...` |
| `services` | `//services/...` minus `//services/workers/training/...` |
| `training_service` | `//services/workers/training/...` |
| `apps` | `//apps/...` |
| `research` | `//research/...` |
| `platform` | `//architecture/...`, `//ci/...`, `//docs/...`, `//examples/...`, `//infra/...`, `//qualification/...`, `//security/...`, `//tools`, `//tools/analysis/...`, `//tools/codegen/...`, `//tools/dev/...`, `//tools/license/...`, `//tools/qualification/...` |
| `build_support` | `//tools/build/...` |
| `release_support` | `//tools/release/...` |
| `test_support` | `//tests/...` |
| `root_support` | `//` (the repository root package only) |

`platform` does not own all of `tools/`. `tools/build`, `tools/release`, the
repository root, and `tests/` are four separate support domains. The split
exists so that production access to a test runner or a release helper does not
imply access to arbitrary CI, infrastructure, or developer tooling.

## Allow matrix

`BAZEL_LAYER_ALLOW_MATRIX` is fail-closed: a direction that is not listed is
forbidden even if no past incident named it. These are the complete destination
sets, not a summary — read the row for the edge you need rather than inferring
one from the flow sketch below.

| Source domain | Allowed destination domains |
| --- | --- |
| `foundation` | `foundation`, `build_support`, `root_support`, `test_support` |
| `offline` | `foundation`, `offline`, `build_support`, `release_support`, `root_support`, `test_support` |
| `training` | `foundation`, `offline`, `training`, `build_support`, `release_support`, `root_support`, `test_support` |
| `runtime` | `foundation`, `offline`, `runtime`, `build_support`, `release_support`, `root_support`, `test_support` |
| `services` | `foundation`, `offline`, `runtime`, `services`, `build_support`, `release_support`, `root_support`, `test_support` |
| `training_service` | `foundation`, `offline`, `training`, `training_service`, `build_support`, `release_support`, `root_support`, `test_support` |
| `apps` | `foundation`, `apps`, `build_support`, `release_support`, `root_support`, `test_support` |
| `research` | every domain |
| `platform`, `build_support`, `release_support`, `test_support`, `root_support` | every domain |

Consequences that are easy to assume the other way round:

- `apps` reaches six of the thirteen domains, not two. It may not reach
  `services`, `training_service`, `runtime`, `offline`, `training`, `research`,
  or `platform`; an app reaches platform capability through `//sdk/...` and
  `//protocols/...`, which are `foundation`.
- `foundation` is the only production domain that may not reach
  `release_support`.
- `evaluation` is `offline`, not a peer of `training` and `serving`. It may not
  import `control/`, `serving/`, `training/`, or any service.
- `training` and `runtime` are siblings, not tiers. `training` may not import
  `control/` or `serving/`, and `runtime` may not import `training/`.
- No production domain may reach `platform`, so nothing under `libs/`,
  `control/`, `serving/`, `services/`, or `apps/` may depend on `infra/`,
  `ci/`, `security/`, or `tools/analysis`.

`services/workers/training` is the sole carve-out from the general services
package group under [ADR-0025](../design/adr-0025-training-service-composition-layer.md). Its
composition targets may import the authoritative training implementation, but
that permission does not apply to any other service and does not allow training
or reusable code to import a deployable. The source of truth expresses the
carve-out with Bazel package-group exclusion syntax; the graph checker applies
the same include/exclude semantics and still requires exactly one match.

## Top-level flow

This sketch is for orientation. It is *not* the rule that is enforced; the allow
matrix above is. Resolve a specific edge from the matrix, never from this
diagram.

```text
protocols
    -> generated bindings and foundational libraries
    -> data / preprocessing / kernels / models / evaluation
    -> training  |  control / serving
    -> services
    -> apps through SDKs and contracts only
```

Two places where the sketch is coarser than the matrix:

- `evaluation` belongs with `data`, `preprocessing`, `kernels`, and `models` in
  the `offline` domain. It is not a peer of `training` and `serving`.
- `training` is not below `services` for most services. Only
  `services/workers/training` (`training_service`) may import `//training/...`.

Supporting directions the matrix settles:

```text
research -> production packages       allowed
production packages -> research       forbidden
infra -> release manifests/targets    allowed
source packages -> infra              forbidden
apps -> generated SDKs/contracts      allowed
apps -> services                      forbidden
```

Research may depend on production so experiments can evaluate real components;
production must consume only promoted contracts and implementations.

One rule frequently listed alongside these is **not** enforced by the Bazel
matrix: a model family importing a sibling family. Every package under `models/`
is `offline`, and `offline -> offline` is allowed, so the sibling-family rule is
architectural intent carried by review. No checker rejects it today, and
`architecture/dependency_budgets.toml` declares no budget under `models/`.

## Go layers

`libs/go/LAYERS.md` is authoritative for which package sits in which layer.
`tools/analysis/check_go_layers.py` carries its own classifier and enforces the
direction between the layers it recognizes; it does not verify a package against
LAYERS.md, and a package its classifier does not recognize has its `libs/go`
imports checked by nothing. Adding a package means updating both.

```text
Layer 0  clock, faults, identifiers
Layer 1  audit, auth, idempotency, messaging contracts, pagination,
         request metadata, resource versions, signing, narrow storage contracts
Layer 2  config, observability, retry, servicekit, durable coordination loops
Layer 3  PostgreSQL/GCS/Redis/Kubernetes/Pub-Sub provider adapters
Layer 4  HTTP, Connect, and gRPC transports
Layer 5  control domains, services, operators, and workers outside libs/go
```

A package root and its provider subpackages sit in different layers. `messaging`
is a Layer 1 contract while `messaging/pubsub` is a Layer 3 adapter, and the
same contract/adapter split applies to `storage/*`, `audit`, and `idempotency`.
`coordination/*` splits the same way one layer up: the loops themselves are
Layer 2 and only their `postgres`/`memory` backings are Layer 3.

Lower layers never import higher layers. `libs/go` never imports `control/`,
`services/`, `data/`, models, or executable packages.

## Service law

A service may consume protocols, libraries, control/domain engines,
orchestration, and specialized model/data packages. Reusable code must not
import a deployable. A process entry point should contain configuration,
provider construction, registration, and exit policy—not domain algorithms.

The Python inference stack mirrors the Go split: `serving/` holds the reusable
libraries and `services/workers/*` the deployable wiring that imports them, the
same way `control/` holds durable domain policy and `services/control_plane`
composes it.

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
- `tools/analysis/check_dependency_layers.py` for the source-level Go prefix
  rules and for the Python `models/ -> research` ban, and
  `tools/analysis/check_dependency_budgets.py` for the Go, Rust, and TypeScript
  per-component prefix allowlists;
- promoted-foundation and paved-road checks;
- `servicekit/production` role capability validation;
- cross-language golden tests;
- no-host-tool and Bzlmod/Nix toolchain checks;
- two independent dumping-ground bans: `check_libs_go_admission.py` rejects any
  new *top-level* `libs/go/*` directory absent from `allowed_top_level` and bans
  the thirteen reserved names in `forbidden_names` (`common`, `shared`,
  `helpers`, `utils`, and nine more), while `check_go_layers.py` separately
  rejects a directory named `common`, `helpers`, `shared`, or `utils` at *any*
  depth under `libs/go`.

The query check is language-independent: Go, Rust, Python, generated targets,
and filegroups are governed by the same graph. Source-level checks remain in
place for language semantics that BUILD metadata cannot express.

### Exception schema

An exception is one entry in `BAZEL_LAYER_EXCEPTIONS`. The checker validates all
of the following, and an entry that omits or exceeds any of them fails CI:

- the **key** is one exact `//source -> //target` edge between two rule labels.
  Package patterns and wildcards never match a real edge, so they fail as stale
  rather than granting anything;
- the **value** holds exactly the four keys `owner`, `adr`, `reason`, and
  `expires_on`. A missing key fails; an extra key fails too;
- `owner` names a team declared in `OWNERS.toml`;
- `adr` matches `ADR-NNNN` and the corresponding `docs/design/adr-NNNN-*.md`
  records `- **Status:** Accepted`;
- `reason` is a non-empty rationale;
- `expires_on` is an ISO `YYYY-MM-DD` date that is neither in the past nor more
  than 90 days ahead of the validation date;
- the edge still exists in the graph. An exception whose edge is gone is
  reported as stale and fails.

`BAZEL_LAYER_EXCEPTIONS` is currently empty. A permanent new dependency
direction is a policy change, not an exception: it requires an ADR plus updates
to the central matrix, this document, and ownership routes.

Run the same gates locally with:

```bash
tools/dev/nixw develop .#ci-bazel --command python3 tools/analysis/check_bazel_layers.py
tools/dev/nixw develop .#ci-bazel --command \
  tools/dev/bazelw build //... --nobuild --config=ci
```

## Retired Rust compatibility crates

The uploaded Rust foundation historically exposed `clock`, `retry`,
`resource_version`, `observability`, `artifact_manifest`, `byte_spec`, and
`python_bindings` as standalone crates. The canonical architecture consolidated
those implementations into `runtime_core`, `telemetry`, `manifests`, `bytes_io`,
and `python_bridge` under
[ADR-0019](../design/adr-0019-rust-runtime-consolidation.md).

The compatibility epoch that ADR-0019 opened is **closed**. The seven crates are
**removed**, not deprecated: there is no `libs/rust/<name>` directory, no
workspace member in the root `Cargo.toml`, and no `Cargo.lock` entry for any of
them. There are no re-export facades left to depend on, so an import of
`mindclade_clock`, `mindclade_retry`, `mindclade_resource_version`,
`mindclade_observability`, `mindclade_artifact_manifest`, `mindclade_byte_spec`,
or `mindclade_python_bindings` does not resolve at all.

`tools/analysis/check_code_docs_alignment.py` keeps them gone. It fails if a
`libs/rust/<name>` directory reappears, and separately if any `.rs` or
`Cargo.toml` file under `libs/rust` so much as names one of the seven crates —
the second clause is the one tripped by copying an old import into a new file.

# Mindclade Internal Monorepo

Production-oriented Bazel monorepo for biomolecular data ingestion, preprocessing, model training, evaluation, and inference.

## Language ownership

```text
Go        fleet control plane and durable policy
Rust      online/runtime data plane and node execution
Python    scientific, model, training, inference, and evaluation numerics
TileLang  qualified accelerator kernels
TypeScript product surfaces and public web clients
```

## Build ownership

Bazel owns build, test, generation, images, qualification, and release outputs. Nix owns pinned toolchains and execution environments. The repository is Bzlmod-only.

## Materialization status

The repository now contains substantive implementation across the Go foundation/control plane, the adopted and deepened Rust runtime/node foundation, runtime authority/routing contracts, artifact/reference/evidence domains, deterministic Python configuration resolution, and preprocessing contracts/DAG/provenance seams. Other paths may still be scaffolded or unqualified; a file existing never implies production readiness.

Start with `docs/architecture/system-design-reference.md`, then consult `components.toml`, `maturity.toml`, `REPOSITORY_STATUS.md`, `SCAFFOLD_STATUS.md`, `VALIDATION.md`, and `QUALIFICATION.md`.


## Runnable Go integrations

Three real local integrations exercise the completed foundation without external
provider credentials:

```bash
go run ./examples/go/control_plane_api/cmd/control-plane-api
go run ./examples/go/event_dispatcher
go run ./examples/go/ingestion_coordinator
```

The control-plane API demonstrates bounded HTTP handling, request lineage, audit, and a transactional-outbox-shaped write boundary. The event dispatcher demonstrates outbox-to-broker publication through `servicekit/production`. The ingestion coordinator demonstrates fenced
leadership, leased work, monotonic cursor commit, and transactional-outbox
boundaries. See `docs/guides/go-integration-examples.md`.

## Go foundation golden paths

The implemented Go foundation is documented in:

- `libs/go/README.md`, `libs/go/LAYERS.md`, and `libs/go/USAGE.md`;
- `docs/architecture/go-foundation.md`;
- `docs/guides/go-foundation-adoption.md`;
- `docs/guides/go-service-golden-path.md`;
- `services/control_plane/internal/bootstrap/` and
  `services/control_plane/internal/foundation/`.

Inspect the two primary reference integrations without provider credentials:

```bash
go run ./services/control_plane/cmd/api --describe-profile
go run ./services/control_plane/cmd/ingestion_controller --describe-profile
```

Both commands emit the exact production capability contract and the expected
`libs/go` package inventory. Their normal execution path fails closed until a
service-owned production adapter factory replaces the scaffold factory.

See `VALIDATION.md` for the exact qualification performed and the connected
provider checks still required.

## Go foundation qualification

Run the hermetic-within-the-checkout foundation lane:

```bash
tools/qualification/go/validate.sh offline
```

Connected CI should run `tools/qualification/go/validate.sh connected` after
providing the pinned module mirror, Bazel/Nix toolchains, and provider test
environments described in `VALIDATION.md`.

## Primary documentation

- [`SCAFFOLD_STATUS.md`](SCAFFOLD_STATUS.md) — implemented versus reserved boundaries
- [`QUALIFICATION.md`](QUALIFICATION.md) — completed and required qualification
- [`docs/qualification/README.md`](docs/qualification/README.md) — shipped validation evidence
- [`docs/architecture/system-design-reference.md`](docs/architecture/system-design-reference.md) — canonical cross-system design
- [`docs/architecture/system-design-traceability.md`](docs/architecture/system-design-traceability.md) — design-to-code/evidence map
- [`docs/architecture/system-overview.md`](docs/architecture/system-overview.md)
- [`docs/architecture/go-foundation.md`](docs/architecture/go-foundation.md)
- [`libs/go/CONSUMPTION.md`](libs/go/CONSUMPTION.md)
- [`docs/examples/control-plane-api.md`](docs/examples/control-plane-api.md)
- [`docs/examples/ingestion-coordinator.md`](docs/examples/ingestion-coordinator.md)
- [`docs/blueprint/production-monorepo-blueprint.md`](docs/blueprint/production-monorepo-blueprint.md)


## Architecture decisions and Go module usage

The accepted decision register and complete Go module guidance are available at:

- `docs/design/decision-register.md`
- `docs/guides/libs-go-module-reference.md`
- `docs/guides/libs-go-recipes.md`
- `libs/go/CONSUMPTION.md`

Architecture, runbook, security, and readiness pages describe the target-state
behavior even where the broader code remains a scaffold.

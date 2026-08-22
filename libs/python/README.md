# Python foundational libraries

`libs/python` contains reusable, process-local mechanisms for Python 3.14. It has no HTTP
routes, CLI entry points, provider construction, databases, or deployment composition roots;
those belong to `services/`, domain packages, or SDKs.

## Maturity and ownership

`components.toml` is authoritative. The path is intentionally mixed rather than promoted as
one umbrella component.

| Package | Status | Owner | Stable contract |
|---|---|---|---|
| `errors` | implemented | platform-control | fault codes, disclosure, retry intent |
| `identifiers` | implemented | platform-control | digests, UUIDv7 IDs, versions, artifact identity |
| `serialization` | implemented | platform-control | deterministic JSON and line bytes |
| `config` | implemented | platform-control | bounded TOML composition, overrides, provenance, digest |
| `worker_runtime` | implemented | platform-control | validated DTOs, cooperative deadlines/cancellation |
| `artifacts` | implemented | runtime-platform | bounded byte/reference/manifest verification and lineage |
| `distributed` | implemented | training-platform | environment, topology, rendezvous, and health validation |
| `geometry` | implemented | biology-ml | bounded finite float64 rigid geometry |
| `observability` | implemented | platform-control | redacted provider-neutral logs, metrics, and trace values |
| `testing` | implemented | biology-ml | bounded numeric, environment, device, rank, and process helpers |

The five newest packages were promoted by explicit repository-owner direction before their first
in-tree consumer. Their APIs are intentionally narrow and provider-free; broadening them still
requires consumer evidence under `ADMISSION.md`.

## Dependency layers

```text
Layer 0  errors
Layer 1  identifiers, serialization
Layer 2  artifacts, config, distributed, geometry, observability, worker_runtime
Layer 3  testing
```

Lower layers do not import higher ones. Cross-process data uses versioned contracts under
`protocols/`; Python dataclasses are validated process-local projections, not alternate wire
formats.

## Tooling and validation

The root uv lock is the developer dependency source of truth. Bazel uses independently
hash-pinned `requirements.lock.txt` and `requirements.darwin.lock.txt` inputs so Linux and
Darwin PyTorch metadata cannot be merged across platforms. The repository is not installed as
a wheel (`tool.uv.package = false`); Bazel `py_library` targets are the supported internal
consumption path. Implemented packages include `py.typed` in their runfiles.

Run the production checks from the repository root:

```bash
uv run --frozen pytest libs/python tests/integration/cross_language
uv run --frozen ruff check libs/python
uv run --frozen ruff format --check libs/python
uv run --frozen mypy libs/python
tools/dev/bazelw test //libs/python/... --config=ci
```

These are libraries rather than deployment-facing code, so they do not carry an SLO, runbook,
release target, migration, or `PRODUCTION_READINESS.md`. A service that composes them owns that
operational evidence.

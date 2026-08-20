# Python foundational libraries

`libs/python` contains reusable, process-local mechanisms for Python 3.12. It has no HTTP
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
| `worker_runtime` | implemented | platform-control | validated stage/workload DTOs and engine delegation |
| `artifacts` | scaffolded | runtime-platform | reserved; no production API |
| `distributed` | scaffolded | training-platform | reserved; no production API |
| `geometry` | scaffolded | biology-ml | reserved; no production API |
| `observability` | scaffolded | platform-control | reserved; no production API |
| `testing` | scaffolded | biology-ml | reserved; no production API |

The five reserved packages have no in-tree consumer from which to derive a stable contract.
Their files remain honest scaffolds under the admission rules in `ADMISSION.md`; importing a
`SCAFFOLD_PATH` constant is not a production capability.

## Dependency layers

```text
Layer 0  errors
Layer 1  identifiers, serialization
Layer 2  config, worker_runtime
```

Lower layers do not import higher ones. Cross-process data uses versioned contracts under
`protocols/`; Python dataclasses are validated process-local projections, not alternate wire
formats.

## Tooling and validation

The root uv lock is the dependency source of truth, and `requirements.lock.txt` is its
hash-pinned Bazel export. The repository is not installed as a wheel (`tool.uv.package =
false`); Bazel `py_library` targets are the supported internal consumption path. Implemented
packages include `py.typed` in their runfiles.

Run the production checks from the repository root:

```bash
uv run --frozen pytest libs/python tests/integration/cross_language
uv run --frozen ruff check libs/python
uv run --frozen ruff format --check libs/python
uv run --frozen mypy libs/python
bazel test --config=ci //libs/python/...
```

These are libraries rather than deployment-facing code, so they do not carry an SLO, runbook,
release target, migration, or `PRODUCTION_READINESS.md`. A service that composes them owns that
operational evidence.

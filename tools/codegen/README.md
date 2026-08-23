# Tools / Codegen

- **Status:** Partly materialized. Three tools are implemented; six remain target-state
  scaffolds and no production capability is claimed for those.
- **Primary implementation ownership:** Bazel/Nix/Python/Go/Rust development and qualification tooling

## Purpose

Repository-owned code generation and the gate that makes `protocols/` the wire authority rather
than a claim. Tools are invoked through Bazel targets in production/CI paths.

## What is here

| Tool | State | What it does |
| --- | --- | --- |
| `verify_generated.py` | implemented | Fails when a committed generated artifact does not match what `protocols/` would produce today. |
| `generate_typescript_sdk.py` | implemented | Regenerates the public TypeScript SDK in place; `--check` is `pnpm generate:check`. |
| `generate_event_catalog.py` | implemented | Projects `protocols/events/catalog.yaml` to `asyncapi.yaml`; `--check` gates it. |
| `generate_proto.sh` | scaffold | Bazel's `go_proto_library`/`py_proto_library` generate Go and Python bindings at build time; nothing is committed. |
| `generate_go_sdk.py`, `generate_python_sdk.py`, `generate_openapi_clients.py`, `generate_jsonschema.py`, `generate_config_schema.py`, `generate_build_files.py` | scaffold | Reserved boundaries. They hold a `SCAFFOLD_PATH` constant and produce nothing. |

## The drift gate

`.gitattributes` marks generated artifacts `linguist-generated=true`, which collapses them in
pull-request diffs. That makes them the files a reviewer is told not to read, so they are exactly
the files that need a machine looking at them. `verify_generated.py` is that machine.

```sh
tools/dev/nixw develop .#ci --command python3 tools/codegen/verify_generated.py
tools/dev/nixw develop .#ci --command python3 tools/codegen/verify_generated.py --mode static
tools/dev/nixw develop .#ci --command python3 tools/codegen/verify_generated.py --repeat
```

Two lanes, with different costs and different authority.

**`--mode regenerate`** is the authority. It runs the pinned generators into a temporary
directory and byte-compares the result against what is committed, so nothing satisfies it except
output a generator actually produced. It needs `node_modules`
(`pnpm install --frozen-lockfile`), and it never writes to the working tree — unlike
`generate_typescript_sdk.py --check`, which regenerates in place and compares digests around the
call, leaving the tree rewritten whether it passed or failed.

**`--mode static`** is hermetic: standard library only, no subprocess, no network, no
`node_modules`. `run_architecture_checks.py` calls `verify_generated.check()`, so drift fails
`ci/presubmit/pipeline.py --static-only` on every pull request without a Node toolchain in the
lane. Byte comparison is unavailable there, so it cross-examines three independent witnesses to
the same contract: the `.proto` source, the base64 `FileDescriptorProto` that protoc-gen-es bakes
into every `_pb.ts`, and the emitted TypeScript. The descriptor is the load-bearing one — it is a
compiled artifact of the `.proto` and not something anyone edits by hand to agree with a lie told
in the source or in the code.

`GENERATED_RULES` in that file claims every `linguist-generated=true` pattern in `.gitattributes`
and records what happens to it: regenerated and compared, required to be absent because Bazel
produces it at build time, or unverifiable with the gate that does own it named. A new rule that
nothing claims is a failure, because a generated artifact no gate looks at is the defect this
tool exists to prevent.

Remediation is always `pnpm generate` (or `python3 tools/codegen/generate_event_catalog.py`), not
an edit to the generated file.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Materialization requirements

Before a remaining scaffold boundary is treated as implemented, add:

- a named owner and reviewed stable contract;
- implementation with bounded resources, cancellation, and deterministic or
  explicitly statistical behavior;
- package-local tests plus required integration/numerical/security evidence;
- a Bazel target using the pinned Nix toolchain environment;
- explicit inputs, outputs, compatibility, failure, retry, and rollback rules;
- documentation of limits and non-responsibilities;
- `PRODUCTION_READINESS.md` evidence for deployment-facing code.

See the architecture chapter for this domain and `SCAFFOLD_STATUS.md` for the
artifact-wide implementation status.

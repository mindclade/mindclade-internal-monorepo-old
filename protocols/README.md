<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Architecture](../docs/README.md) · [Maturity](../SCAFFOLD_STATUS.md)

# Protocols and compatibility

> **Maturity:** Mixed; each schema surface carries its own compatibility and
> qualification evidence.
> **Primary implementation:** Protobuf, OpenAPI, AsyncAPI, and explicit mapping
> policy.

`protocols/` is the authority for cross-process and cross-language wire
contracts. A concept can have several external projections, but every field has
one authority or a tested mapping.

All promoted protobuf packages compile through `//protocols:all_protos`.
`//protocols:protobuf_governance_test` checks the complete descriptor surface,
removed-symbol tombstones, Go package identity, and per-RPC auth/deadline/retry/
idempotency policy. `//protocols:typescript_projection_test` proves that every
canonical source has exactly one checked-in Protobuf-ES projection.
`//protocols:protobuf_contract_image` packages those exact sources, the
descriptor set, compatibility baseline, and maturity policy into one
digest-addressed OCI release subject. `protobuf-contracts` is a closed catalog
target in the reviewed release-request pipeline; it is not published by
presubmit or by developer commands.

## What's here

| Path | Responsibility |
| --- | --- |
| [`proto/`](proto/) | Canonical Protobuf service and message definitions |
| [`openapi/`](openapi/) | Public and administrative HTTP API projections |
| [`events/`](events/) | Event catalog, AsyncAPI surface, and generated event bindings |
| [`mappings/`](mappings/) | Explicit identifier, error, timestamp, event, and API mappings |
| [`compatibility/`](compatibility/) | Breaking-change policy, reserved fields, and runtime compatibility |
| [`rust/`](rust/) | Rust protocol bindings and compatibility crate |

## Boundary

- Protocols define representation and compatibility, not domain policy.
- Domain decisions belong in [`control/`](../control/); runtime execution
  belongs in [`services/`](../services/).
- Generated code is derived output. Change the authoritative schema or mapping,
  then regenerate through the repository-owned Bazel target.
- Field renames, removals, reuse, and semantic changes require compatibility
  review and evidence.

## Generated language bindings

Bazel is the generation authority. There is no committed `protocols/gen/` tree: every
binding is an action output, so the only way to consume a contract is to depend on the
target that generates it. That is deliberate — a checked-in binding can be edited, and an
edited binding is a fork of the wire.

| Language | Rule | Target naming |
| --- | --- | --- |
| Go | `go_proto_library` | `<package>_go_proto`, one per proto package |
| Python | `py_proto_library` | `<subject>_py_pb2` |
| Rust | [`rust/`](rust/) | compatibility crate over `filegroup` proto sources |
| TypeScript | `buf.gen.yaml` | checked-in projection under `sdk/typescript` |

Go and Python differ in a way that matters when adding a contract. `protoc` emits one Go
package per proto package, so a `go_proto_library` that names the package covers it. It
emits one Python *module per `.proto` file*, and the generating aspect only reaches what is
listed in `deps` — so a `py_proto_library` naming a subset of its package's
`proto_library` targets builds green and fails later, at import, in a Python caller. List
every `proto_library` in the package.

Both halves are pinned by conformance tests in [`consumers/`](consumers/):

```bash
nix develop .#ci-bazel --command tools/dev/bazelw test //protocols/consumers/... --config=ci
```

`generated_go_test.go` imports all ten generated Go packages and asserts each registers a
canonical message name. `generated_python_test.py` does the same for Python, then
partitions every promoted `.proto` into modules a `py_proto_library` reaches and modules no
Python caller can import, asserting that partition by set equality in both directions. A
new `.proto` with no Python binding fails it; so does closing one of the pinned gaps
without shrinking the constant that records them.

## Start here

- [`events/README.md`](events/README.md) for event authority and generation
- [`openapi/README.md`](openapi/README.md) for HTTP projections
- [`mappings/README.md`](mappings/README.md) for cross-projection rules
- [ADR-0014: protocol authority](../docs/design/adr-0014-protocol-authority.md)

Before changing a durable contract, follow [`CONTRIBUTING.md`](../CONTRIBUTING.md)
and update the governing ADR, compatibility fixtures, and downstream generated
surfaces together.

## Validation

Run the same pinned schema checks used by presubmit:

```bash
nix develop .#ci-lint --command buf lint protocols
nix develop .#ci-lint --command buf breaking protocols \
  --against .git#branch=main \
  --against-config '{"version":"v2","modules":[{"path":"protocols/proto"}]}'
```

The second command compares the working tree with `main`; change the branch to
the pull request base when needed. The compatibility profile and the two
file-scoped legacy naming exceptions are governed by
[`buf.yaml`](buf.yaml) and
[`compatibility/breaking-policy.yaml`](compatibility/breaking-policy.yaml).
`buf.gen.yaml` generates the checked-in Protobuf-ES consumer surface under
`sdk/typescript/src/generated/proto`. The generated tree and the public OpenAPI
types are verified for deterministic regeneration by `pnpm run generate:check`.
Bazel remains the build, generation-graph, verification, and provenance
authority. Buf is the pinned TypeScript projection engine and pnpm is only a
developer wrapper; generated files are never edited by hand.

Connected deployment qualification is defined in
[`qualification/README.md`](qualification/README.md). It is intentionally
separate from source maturity: local success cannot manufacture authenticated
service or release-provenance evidence.

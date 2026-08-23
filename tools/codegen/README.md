# Tools / Codegen

- **Status:** Implemented for every artifact this directory owns. One reserved boundary
  (`generate_proto.sh`) remains; no production capability is claimed for it.
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
| `generate_jsonschema.py` | implemented | Projects the protobuf descriptor surface to `protocols/events/generated/**`; `--check` gates it. |
| `generate_proto.sh` | scaffold | Bazel's `go_proto_library`/`py_proto_library` generate Go and Python bindings at build time; nothing is committed. |

Five further tool paths were reserved here and have been removed rather than implemented,
because each artifact they named already has an authority. See
[Generation this directory does not own](#generation-this-directory-does-not-own).

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

## Generators

Most files here are still scaffold boundaries holding a `SCAFFOLD_PATH` constant. The ones with
an implementation behind them are listed below; anything not listed generates nothing yet, and
its absence is why the surface it would own is hand-maintained.

| Tool | Reads | Writes | Drift gate |
| --- | --- | --- | --- |
| `generate_event_catalog.py` | `protocols/events/catalog.yaml` | `protocols/events/asyncapi.yaml` | `//tools/codegen:event_catalog_generated_test` |
| `generate_jsonschema.py` | `protocols/compatibility/protobuf-v1-descriptor.json` + `protocols/mappings/event_proto.yaml` | `protocols/events/generated/**.schema.json` | `--check`, asserted by `tools/codegen/tests/test_generate_jsonschema.py` |
| `generate_typescript_sdk.py` | `protocols/` through pinned `buf` and `openapi-typescript` | `sdk/typescript/src/generated/` | `--check`, run by `pnpm generate:check` |

### `generate_jsonschema.py`

```sh
tools/dev/nixw develop .#ci --command python3 tools/codegen/generate_jsonschema.py
tools/dev/nixw develop .#ci --command python3 tools/codegen/generate_jsonschema.py --check
```

The event schemas under `protocols/events/generated/` are derived output. The derivation chain
is `protocols/proto/**.proto` → protoc (`//protocols:protobuf_descriptor_set`) →
`protocols/compatibility/protobuf-v1-descriptor.json`, which
`//protocols:protobuf_governance_test` compares byte-for-byte against the sources →
`protocols/events/generated/**.schema.json`, which `--check` compares byte-for-byte against the
projection. Both links fail closed, so a `.proto` change that has not reached the schemas is a
build failure rather than a silent producer/consumer disagreement.

Changing a schema means changing the `.proto` or `protocols/mappings/event_proto.yaml` and
regenerating. The policy may only *refine* what the descriptor says — tighten a range, add a
pattern, declare which fields are required — and the generator rejects a refinement that names a
field the message no longer has, contradicts a projected JSON type, or widens a protobuf range.
Every string, bytes, repeated, and 64-bit integer projection must declare a bound; 64-bit
integers may not exceed 2^53-1, because these schemas project them as JSON numbers.

The generator reads the descriptor surface rather than parsing `.proto` text, because parsing
protobuf source would be a second, unreviewed implementation of what protoc already decides. The
descriptor surface is protoc's own answer, and it is already gated against the sources.

What it deliberately refuses, rather than guessing at:

- **map fields.** protoc rewrites `map<K, V>` into a repeated synthesized `*Entry` message, and
  the descriptor surface flattens that entry beside declared messages while dropping the
  `map_entry` option and the key type. proto3 JSON writes a map as an object keyed by the map
  key, which cannot be derived from what remains — and an array of `{key, value}` pairs would be
  a schema no producer emits. It raises.
- **groups and unmapped imports.** Same reason: no rule, no default.

Payload unions use `anyOf`, never `oneOf`. Every proto3 field is optional, so payload schemas
overlap by construction and `oneOf` — which requires *exactly* one branch to match — would
reject the documents the union exists to accept.

There is no Bazel test target for the drift check: the descriptor surface is a source of the
`//protocols` package, whose default visibility is private, so a sandboxed target cannot read it.
The check runs in the root pytest lane, which `pyproject.toml` points at `tools/`.

## Generation this directory does not own

`generate_go_sdk.py`, `generate_python_sdk.py`, `generate_openapi_clients.py`,
`generate_config_schema.py`, and `generate_build_files.py` were reserved paths carried from the
blueprint tree, each holding a `SCAFFOLD_PATH` constant and a docstring about "scientific and
numerical behavior" that no BUILD-file emitter was ever going to have. They are removed. The
reason is recorded here because the alternative is that the next reading of the blueprint
reserves them again, and the second implementation attempt has less evidence available than the
first.

**Go and Python protobuf bindings.** ADR-0014 decides this: "Go and Python are Bazel action
outputs; Rust is generated by the Bazel-owned Cargo build script. TypeScript is the one
checked-in language projection because it is an independently published SDK input." The Go
bindings are `go_proto_library` targets with importpath
`go.mindclade.dev/protocols/gen/go/...`, linked and asserted by
`//protocols/consumers:generated_go_test`, which lives in an underscore-prefixed package
precisely so the source-only Go module cannot pick them up. The Python bindings are
`py_proto_library` targets. Neither is committed, and `GENERATED_RULES` requires
`protocols/**/*.pb.go`, `*_pb2.py`, `*_pb2.pyi` and `*_connect.go` to be **absent** — so a
generator emitting them would produce exactly the files the drift gate rejects.

`sdk/go/` and `sdk/python/` are reserved client boundaries, not generated ones. The shape is
already visible in the language that is finished: `sdk/typescript/src/*.ts` is a hand-written
ergonomic client and only `src/generated/` is derived. Both other SDKs are still `SCAFFOLD_PATH`
stubs with no `components.toml` entry and no `OWNERS.toml` owner, so generating into them would
be materializing a scaffold rather than finishing a codegen chain — and `sdk/go/go.mod` says so
itself, deferring its own `go` directive to "when API generation lands".

**OpenAPI clients.** `protocols/openapi/public.openapi.yaml` is already generated end to end by
`generate_typescript_sdk.py` (`openapi-typescript` → `sdk/typescript/src/generated/api.ts`),
gated by `pnpm generate:check` and cross-examined by `verify_generated.py`'s
`check_openapi_binding`. The administrative spec is not orphaned but frozen:
`protocols/openapi/README.md` states that "the administrative projection must remain
non-operational until its independent authorization/audit contract is reviewed", and outside
`protocols/openapi/BUILD.bazel` nothing in the tree references it. Generating a client for it is
the thing that review is a precondition for, not a gap to fill.

**Configuration schemas.** ADR-0014: "Configuration owns one schema per resolved configuration
contract." `configs/README.md` scopes that ownership — `configs/` "owns declarative inputs and
schemas, not resolution behavior", with resolution in `libs/python/config/`. So
`configs/schemas/*.schema.json` is the authority rather than a projection of one, and there is no
upstream artifact to derive it from. Each schema is already paired with a fixture and gated by
`configs/contract_validation.py`, `//configs:test_contracts`, and `check_mlops_contracts.py`.

**BUILD files.** `//:gazelle` owns Go BUILD metadata, `//:gazelle_check` fails on diff, and the
affected selector force-includes that target in every selection. The root `BUILD.bazel` records
why the binary is restricted to `@gazelle//language/go`: the default binary "also carries Proto
and visibility extensions that would silently broaden this target's authority". `ci/bazel/README.md`
then sets the bar for widening it — pinned in `MODULE.bazel`, locked for every supported
platform, included in downloader/mirror qualification, and "proven against the existing Python
graph before its output becomes authoritative". A repository-local emitter is the unreviewed
second authority both texts refuse.

### What was actually missing behind that last one

The protobuf build graph under `protocols/proto/**` is exactly derivable from the `.proto`
sources — 47 sources, 47 `proto_library` rules, and every dependency edge reproducible from the
`import` statements — and nothing re-derived it. A `.proto` added without its rules is not a
build error, and every gate that would notice reads one of the hand-maintained lists rather than
the tree: `buf` reads the directory and passes, `//protocols:protobuf_governance_test` builds its
descriptor set from `//protocols:all_proto_sources`, `//protocols:typescript_projection_test`
takes the same list as its input, and `//protocols:protobuf_contract_image` packages it. Even
`verify_generated.py` does not catch it, because `buf generate` reads the directory too and duly
emits the `_pb.ts` its bijection asks for.

`tools/analysis/check_protocol_graph.py` closes that, as a checker rather than a generator: it
takes no BUILD-metadata authority, cannot rewrite a reviewed file, and its failure names the edge
to add. It is registered in `architecture/enforced_decisions.toml` against ADR-0014 and runs in
`--static-only`.

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

A new tool here additionally needs an artifact that is **checked in**, a **single** authoritative
source it is derived from, and a disposition in `GENERATED_RULES` paired with a
`linguist-generated=true` rule in `.gitattributes` — that coverage is checked in both directions,
so an undeclared generated tree fails and a claimed pattern that leaves `.gitattributes` fails
too. If the artifact is a Bazel action output, it does not belong here; declare the rule instead.

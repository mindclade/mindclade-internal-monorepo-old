# Protocols / Compatibility

- **Status:** Implemented field-compatibility guards for the listed v1 packages; other protocol surfaces remain mixed.
- **Primary implementation ownership:** Protobuf, OpenAPI, event schemas, and compatibility policy

## Purpose

Canonical cross-language wire contracts and explicit mappings. A concept may have multiple external projections, but fields have one authority or a tested mapping. This path specializes that domain for **compatibility**.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Frozen field maps

`runtime_v1_fields.json`, `inference_v1_fields.json`,
`training_v1_fields.json`, `artifact_v1_fields.json`, and
`registry_v1_fields.json` freeze published message names, field numbers, field
names, labels, and declared types. Additive fields are permitted; removal,
rename, relabeling, or retyping requires a versioned migration. Scaffold
placeholders are deliberately excluded so a reserved package can be
materialized without treating the placeholder as domain state.

Regenerate a map only when intentionally accepting the current additive source:

```bash
python3 tests/integration/cross_language/test_wire_compatibility.py training
```

Review and apply the printed JSON; the test never rewrites a baseline.

The shared checkpoint wire fixture is emitted deterministically from the
canonical Python protobuf projection:

```bash
bazel run //protocols/proto/mindclade/training/v1:checkpoint_contract_fixture_generator -- \
  --emit-fixture
```

Review and apply the single-line JSON output. Python, TypeScript, and Rust must
all reproduce those exact bytes before the fixture can be promoted.

## Limits

The field-map check protects message fields, not enum numeric assignments,
validation semantics, service behavior, or unknown-field handling. Buf remains
authoritative for protobuf lint and FILE-level breaking checks. Cross-language
goldens cover selected execution, model, and checkpoint records. The checkpoint
fixture is compared with Python and TypeScript encoders and decoded/re-encoded
by Rust bindings generated from the canonical protobuf sources. A passing map
or golden alone is not qualification or production evidence.

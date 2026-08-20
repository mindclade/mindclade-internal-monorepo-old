# Python canonical serialization

Layer 1 of `libs/python`. Depends on the standard library and
`libs/python/errors`; depends on nothing else here.

## What it owns

The byte encoding of a document, in exactly one form:

| Function | Encoding |
|---|---|
| `canonical_json_bytes` | keys sorted, no insignificant whitespace, UTF-8, no non-finite floats |
| `canonical_lines` | newline-separated with a trailing newline, UTF-8 |
| `canonical_field` | validates one field of a line-oriented document |

## Why this package exists

Five call sites in this tree independently decided how to encode a document
before digesting it, and two of the decisions disagreed. `libs/python/config`
used `ensure_ascii=False`; `preprocessing/cache/keys.py`,
`preprocessing/provenance/manifest.py`, `data/curation/pipeline.py` and
`serving/contracts/runtime_manifest.py` used `ensure_ascii=True`. For a document
containing any non-ASCII byte those produce different bytes, so the same content
had two digests — which is the one thing content addressing may not permit.

UTF-8 is not a preference. Go's `encoding/json` and Rust's `serde_json` both emit
UTF-8, so the ASCII-escaping form also disagreed with the other two languages.

`allow_nan=False` for the same class of reason: `NaN` and `Infinity` are not JSON
and both other languages refuse them, so emitting them would produce a document
only Python can read. Failing at encode time turns a remote parse error into a
local one.

## What it does not own

Identity. This package returns bytes; digesting them belongs to
`libs.python.identifiers`:

```python
from libs.python.identifiers import Digest
from libs.python.serialization import canonical_json_bytes

digest = Digest.of(canonical_json_bytes(document))
```

Keeping the two apart is what stops this package growing a second identity
vocabulary alongside the one `identifiers` already owns.

It also does not sort repeated entries for a line-oriented document. Only the
caller knows which key the Go writer sorts on, and guessing would produce bytes
that differ from the other side's for a reason nothing here could detect.

## Limits and failure behavior

`canonical_field` rejects the two reserved delimiters, `\n` and `|`. Without that
the line encoding is not injective — a field containing a vertical bar could
impersonate a structural line, and two different documents would seal to one
digest.

Everything raises `libs.python.errors.InvalidArgument`, which is also a
`ValueError`. Nothing coerces or truncates.

## Reserved, not implemented

`json.py`, `toml.py`, `yaml.py` and `protobuf.py` were reserved by the target-state
blueprint and are not part of this package. Format-specific decoding has one real
caller — `libs/python/config`, which reads TOML — and ADR-0018 treats a package
created before it has a consumer as a false implementation claim. A future format
module joins this package under the bar in `libs/python/ADMISSION.md`, not before.

# Python identity primitives

Layer 1 of `libs/python`. Depends on the standard library and
`libs/python/errors`; depends on nothing else here.

## What it owns

| Type | Canonical form |
|---|---|
| `Digest` | `sha256:<64 lowercase hex>` |
| `ResourceId` | `<kind>_<32 lowercase hex UUIDv7>` |
| `ResourceVersion` | `rv1:<generation>:sha256:<64 lowercase hex>` |
| `ArtifactRef` | `{digest, size_bytes, media_type, logical_kind, schema_version}` |

These are the text forms that cross a process or language boundary. Go declares
the same four in `libs/go/identifiers` and `libs/go/resourceversion`; the fixtures
under `tests/integration/cross_language/fixtures/` are the shared vectors, and
this package is tested against them directly.

## What it does not own

Where anything is stored. `ArtifactRef` carries no URI, provider, bucket, or
generation, and `test_manifest_roundtrip.py` asserts their absence — ADR-0004
identifies an artifact by what it contains, so the same bytes in two buckets must
be one artifact rather than two.

Which resource kinds exist is a domain decision. This package validates the
*shape* of a kind and leaves the vocabulary to the packages that mint IDs.

## Why parsing is strict

Uppercase hexadecimal is rejected everywhere. A digest and an ID are used as
database keys, cache keys and signed material, so two spellings of one value
would mean a content address that is not a single address.

`is_canonical_digest` is the one predicate that replaces the three the tree
previously disagreed on — a strict regex, `startswith` plus `len == 71`, and
`startswith` alone. The length-based forms accepted uppercase hex that the regex
rejected.

## Limits and failure behavior

Everything raises `libs.python.errors.InvalidArgument`, which is also a
`ValueError`, so callers that already catch `ValueError` keep working. Nothing
here degrades or coerces: a malformed value raises rather than becoming a
zero value, because a silently-zeroed identifier is indistinguishable from a real
one downstream.

Wire widths are enforced: artifact sizes and resource generations are `uint64`,
and artifact schema versions are `uint32`. Advancing a generation already at the
`uint64` maximum raises `OutOfRange`, matching the Go contract. Media types and
logical artifact kinds have one bounded lowercase canonical spelling.

`Digest.of_reader` streams in 1 MiB chunks and returns the byte count alongside
the digest, so hashing a checkpoint does not require reading it into memory.
`Digest.equals` compares in constant time; digests gate artifact admission, and an
early-returning comparison leaks how much of a forged digest was correct.

The UUIDv7 generator is monotonic under a lock within one `IdGenerator` instance
(and therefore within the process using the default instance). Within one millisecond it counts
up through the 12-bit `rand_a` field (RFC 9562 method 2); when that space is
exhausted it advances into the next millisecond rather than reusing a value, and a
backward clock step holds the last stamp and counts forward. IDs never go
backwards or repeat within that generator. Separate processes and machines have independent clocks,
counters, and random fields: UUIDv7 provides useful time locality and uniqueness, not a globally
coordinated lexicographic total order.

## Non-responsibility: absence

Unlike the Go types, none of these carry a presence bit. Go needs one so a zero
value can mean "absent" without making an all-zero digest unrepresentable. Python
spells absence `Digest | None`, so every instance here is a real value.

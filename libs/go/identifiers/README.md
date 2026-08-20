# Mindclade Go Identifiers

`go.mindclade.dev/libs/go/identifiers` provides the foundational identifier
and content-digest types used across Mindclade Go code.

It has no Mindclade-package or third-party dependencies.

## Resource IDs

The canonical resource-ID format is:

```text
<kind>_<32 lowercase hexadecimal UUIDv7 characters>
```

Examples:

```text
run_019c7af21b8276d2a0d522fe41739a21
model_019c7af21b827f53a6b84710f1815c84
```

Kinds contain 2–24 ASCII lowercase letters or digits and begin with a letter.
The payload is the compact byte-order-preserving representation of an RFC 9562
UUIDv7. IDs of the same kind therefore sort by their embedded millisecond
creation time before their monotonic/random field.

```go
runKind := identifiers.MustParseKind("run")
runID, err := identifiers.NewID(runKind)
if err != nil {
    return err
}

parsed, err := identifiers.ParseIDKind(runID.String(), runKind)
```

`Generator` is safe for concurrent use and makes UUIDv7 output strictly
monotonic within one process even when several values are generated in the
same millisecond or the wall clock regresses. Cross-process uniqueness remains
probabilistic and uses `crypto/rand.Reader` by default.

For deterministic tests:

```go
generator, err := identifiers.NewGenerator(
    identifiers.WithTimeSource(func() time.Time { return fixedTime }),
    identifiers.WithEntropySource(bytes.NewReader(entropy)),
)
```

Do not use resource IDs as authorization capabilities. They are opaque stable
identifiers, not secrets.

## UUIDs

The package supports:

- UUIDv4 generation;
- monotonic UUIDv7 generation;
- canonical, compact, and URN parsing;
- version, variant, and UUIDv7 timestamp inspection;
- JSON, text, binary, and `database/sql` integration.

```go
uuid, err := identifiers.NewUUIDv7()
createdAt, ok := uuid.Time()
```

## Digests

`Digest` is a SHA-256 content digest with canonical text:

```text
sha256:<64 lowercase hexadecimal characters>
```

```go
digest := identifiers.SHA256(artifactBytes)

digest, bytesRead, err := identifiers.SHA256Reader(reader)
```

Digests support constant-time equality, JSON/text encoding, and SQL scanning.
A digest verifies byte identity; it does not establish provenance, signatures,
or trust.

## Zero values

- `ID{}` and `Digest{}` represent absence and serialize to JSON `null` and SQL
  `NULL`.
- `UUID{}` is the standards-defined nil UUID and serializes as
  `00000000-0000-0000-0000-000000000000`.

## Errors

The package uses standard Go wrapping and sentinels rather than importing
`libs/go/faults`:

```go
errors.Is(err, identifiers.ErrInvalid)
errors.Is(err, identifiers.ErrInvalidID)
errors.Is(err, identifiers.ErrInvalidDigest)
errors.Is(err, identifiers.ErrEntropy)
```

Callers classify these errors into `faults.Code` at their own boundary.

## Bazel

```text
//libs/go/identifiers:identifiers
//libs/go/identifiers:identifiers_test
```

Do not add a nested `go.mod`; use the monorepo's authoritative Go module.

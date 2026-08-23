# `mindclade_identifiers`

Validated names and Mindclade resource identifiers. `ResourceId` uses the same canonical representation as Go: `<kind>_<32 lowercase UUIDv7 hex characters>`. Cross-language fixture tests guard this wire identity.

One kind grammar, shared by `ResourceKind` and `ResourceId` and agreeing with `libs/go/identifiers` and `libs/python/identifiers`: 2 to 24 characters, `[a-z][a-z0-9]*`. The separator `_` is never a kind character. `tests/integration/cross_language/test_identifiers.py` reads the bounds out of all three languages and fails when one drifts.

The random field is drawn from the operating system CSPRNG, as Go's `crypto/rand` and Python's `secrets` are. Generation is process-local and not an authorization primitive, but it is also not predictable: `tests/entropy.rs` pins that the body is no longer a function of the clock, the pid and a process counter. Uniqueness is probabilistic over 74 random bits; unlike Go's `Generator` and Python's `IdGenerator`, this crate keeps no per-mint state and promises no intra-millisecond ordering.

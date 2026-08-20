# `mindclade_object_store`

Stable conditional object-store contract with integrity-verifying in-memory and
local-filesystem providers. The local provider serializes operations within one
process, atomically publishes each data/metadata file, reserves its internal
metadata namespace, and fails closed on torn data/metadata or digest mismatch.
It is not a cross-process transactional database; deployments needing
multi-process conditional writes must use a qualified provider adapter. Cloud
SDK adapters live in owning services, not the shared foundation, and must pass
the same provider conformance suite.

New local objects use version-2 metadata with a whole-object digest and 4 MiB
chunk digests. Full reads retain whole-object verification; range reads verify
only the chunks they touch, making a small verified read proportional to its
range rather than the complete artifact. Version-1 three-line metadata remains
readable and deliberately falls back to whole-object verification. Rewriting an
object migrates it to version 2.

The Cargo `provider-adapters` feature is enabled by default for compatibility.
Local-only tools may disable default features to avoid linking cloud SDK and
TLS stacks they do not call. This keeps portable probes and local utilities
small without forcing upstream dependency-version convergence.

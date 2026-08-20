# `mindclade_object_store`

Stable conditional object-store contract with integrity-verifying in-memory and
local-filesystem providers. The local provider serializes operations within one
process, atomically publishes each data/metadata file, reserves its internal
metadata namespace, and fails closed on torn data/metadata or digest mismatch.
It is not a cross-process transactional database; deployments needing
multi-process conditional writes must use a qualified provider adapter. Cloud
SDK adapters live in owning services, not the shared foundation, and must pass
the same provider conformance suite.

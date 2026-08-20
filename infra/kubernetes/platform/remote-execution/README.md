# Remote execution boundary

No remote executor or cache image, endpoint, identity, or storage contract exists in this
repository, so this module records the required contract and denies traffic from any workload
labeled as a remote-execution component. It does not conflate build remote execution with the
ticketed ML runtime or node-agent protocols.

Activation requires a pinned server implementation, tenant-aware authentication and cache
isolation, encrypted transport, action-cache integrity, bounded eviction, cost/capacity limits,
and reproducibility tests against the repository's Bazel toolchain. Rollback removes clients'
endpoint configuration before scaling executors down; cache deletion is a separate, reviewed
data-lifecycle action.

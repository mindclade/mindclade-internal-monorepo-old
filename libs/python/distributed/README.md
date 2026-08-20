# Python distributed contracts

This package validates process-local distributed execution inputs without initializing a process
group. `DistributedEnvironment` round-trips the standard rank environment, `ProcessTopology`
provides deterministic rank/coordinate and group mappings, rendezvous objects validate endpoint
and membership bounds, and cluster health evaluation reports missing, stale, unhealthy, and ready
ranks deterministically.

Ranks and world sizes are bounded unsigned values; topology dimensions must multiply exactly to
the world size. Rendezvous hosts, ports, run identifiers, node counts, and timeouts are validated.
Health snapshots reject duplicate ranks, timestamps from the future, and inventories outside the
declared world. Returned mappings and records are immutable snapshots.

This package does not call `torch.distributed`, create sockets, elect leaders, persist rendezvous
state, retry failed ranks, or decide placement/admission policy. Those responsibilities stay in
engines, adapters, and the orchestration control plane. Cross-process messages use the versioned
contracts under `protocols/`, not these Python-private dataclasses.

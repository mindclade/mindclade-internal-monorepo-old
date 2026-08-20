# NCCL transport boundary

The module is intentionally fail-closed: pods labeled for NCCL receive no peer ingress or
egress. A production policy cannot be written safely until qualified images establish exact
rendezvous and data-plane ports, network interfaces, GKE topology, and DNS requirements.

Activation evidence includes multi-node all-reduce correctness and throughput, timeout and
partition behavior, rank failure and cancellation, deterministic/numerical tolerances, topology
placement, and a packet-level review. Rollback re-applies `block-unqualified-nccl`, suspends the
JobSet, and holds Kueue before any node-pool change.

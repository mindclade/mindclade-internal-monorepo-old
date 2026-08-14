# Python worker runtime contracts

This package is the **Python scientific-engine side** of the unified stage-worker
protocol. The canonical wire representation is Protobuf under
`protocols/proto/mindclade/orchestration/v1`; Go owns durable stage policy and
Rust validates signed execution tickets, fencing, resource budgets, deadlines,
and bulk-buffer descriptors before Python is invoked.

This package owns only bounded process-local DTO validation and delegation to an
owning scientific/numerical engine. It intentionally does **not** import
PyTorch, a broker, Kubernetes, a database, or signing libraries.

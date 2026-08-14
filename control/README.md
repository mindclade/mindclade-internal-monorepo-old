# Go control-plane domains

`control/` contains reusable Go **domain policy and durable state machines**.
It sits between generated contracts/`libs/go` mechanisms and deployable service
composition roots.

```text
protocols/generated bindings
        -> libs/go mechanisms
        -> control domain engines
        -> services/control_plane wiring
```

Control packages own validation, resource state, workflow decisions,
repositories as narrow interfaces, policy, and domain events. They do not own
provider clients, process signals, HTTP/gRPC server lifecycle, Rust/Python
execution, model numerics, or scientific preprocessing.

All domains use canonical `faults`, `identifiers`, `requestmeta`, `audit`,
`idempotency`, `resourceversion`, `retry`, transactions, coordination, and
observability as appropriate. Durable writes emit events through the shared
transactional outbox. Long-running work uses shared fenced leases/work queues.

The current code provides production-shaped domain contracts and tests; a
service is not deployable until its `services/control_plane` factory wires real
providers and passes `PRODUCTION_READINESS.md`.

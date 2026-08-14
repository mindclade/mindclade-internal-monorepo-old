# Control-plane service internals

This directory contains service-owned composition and transport wiring for the
Go modular control plane. Reusable durable policy lives under `control/`;
reusable mechanisms live under `libs/go/`.

```text
bootstrap/   stable process roles, command entry, capability manifests,
             fail-closed provider factory, servicekit/production assembly
config/      service-specific typed projection of the strict config snapshot
foundation/  typed aggregate of approved Go mechanisms/adapters
transport/   HTTP/Connect/gRPC composition over generated contracts
```

Domain modules and generated handlers must not be moved into `libs/go` merely
for convenience. Provider clients are constructed by the service factory and
exposed through narrow foundation contracts. Command roots contain no domain
logic and no lifecycle/signal implementation.

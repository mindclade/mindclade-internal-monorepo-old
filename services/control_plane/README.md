# Control plane service

The control plane is a set of Go process roles built from one domain layer and
one shared foundation. It is a modular monolith at the code and persistence
boundary; roles can be deployed independently without copying policy or
coordination mechanisms.

Reusable policy and state machines live under `control/`. Reusable mechanisms
live under `libs/go`. This directory contains only process composition,
provider wiring, transports, migrations, and deployment-facing tests.

Every command must use `internal/bootstrap`, which delegates to
`servicekit/production.Builder`. The generated commands fail closed until their
service-owned production factories are implemented.

See `GO_FOUNDATION_CONSUMPTION.md` and `PRODUCTION_READINESS.md`.

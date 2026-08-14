# ADR-0014: Explicit protocol source-of-truth matrix

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Cross-language and public contracts

## Context

The platform uses Protobuf, OpenAPI, durable events, JSON projections, AsyncAPI,
configuration schemas, and generated SDKs. Hand-maintaining the same resource
in several formats would create field, enum, error, identifier, pagination, and
compatibility drift.

## Decision

- Protobuf is canonical for internal RPC, internal durable events, runtime
  tickets/snapshots, and worker protocols.
- OpenAPI is canonical for the public REST surface.
- JSON Schema and AsyncAPI projections are generated or mapped explicitly where
  JSON/event catalog delivery is required.
- Configuration owns one schema per resolved configuration contract.
- Go, Rust, Python, and TypeScript bindings/clients are Bazel-generated; sources
  are committed only when independently published artifacts require it.
- A concept exposed in several protocols has one authoritative definition or an
  explicit mapping with compatibility tests.

## Consequences

Compatibility gates cover field removal/reuse, enum evolution, requiredness,
IDs, timestamps, pagination, idempotency, error mapping, unknown fields, event
replay, and signature canonicalization.

## Rejected alternatives

Independent hand-written models in every language/protocol and a single
protocol forced onto all public/internal/configuration use cases were rejected.

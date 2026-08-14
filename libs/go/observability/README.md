# observability

`observability` is Mindclade's provider-neutral telemetry composition layer.
It standardizes safe structured logging, low-cardinality metric records,
request and trace propagation coordination, resource metadata, and telemetry
provider lifecycle without installing global state.

Concrete OpenTelemetry SDKs and exporters are composed by service binaries or
infrastructure adapters. The core package intentionally does not hide or fork
OpenTelemetry APIs.

## Responsibilities

- context-enriched and secret-redacting `log/slog` handlers;
- public-safe fault logging;
- immutable resource and attribute conventions;
- non-PII principal and request-lineage correlation;
- trace-ID correlation through an injected provider;
- low-cardinality metric measurement contracts;
- provider propagation coordinated with `requestmeta`;
- ordered flush and reverse-order shutdown;
- panic isolation at best-effort telemetry boundaries;
- deterministic recorders under `observability/obstest`.

## Non-responsibilities

- OTLP endpoint configuration;
- exporter or collector implementation;
- process-wide OpenTelemetry globals;
- custom trace sampling;
- custom span storage;
- dashboards, alerts, or service-specific SLO definitions.

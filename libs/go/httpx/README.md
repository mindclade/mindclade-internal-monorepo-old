# HTTP Transport (`httpx`)

Production HTTP server, client, propagation, JSON, and RFC 9457-compatible
error-envelope primitives. The package never opens listeners, exits a process,
performs retries, or owns application authentication/authorization policy.

Transport middleware lives in `httpx/middleware`; lifecycle health endpoints
live in `httpx/health`; optional official OpenTelemetry wrappers live in
`httpx/otel`. Connect routes should normally use `connectx/otel` rather than
being double-instrumented with `otelhttp`.

# HTTP OpenTelemetry adapter

Optional wrappers around the official `otelhttp` handler and transport. Apply
this package to ordinary HTTP routes. Connect RPC routes should normally use
`connectx/otel` instead to avoid duplicate server spans.

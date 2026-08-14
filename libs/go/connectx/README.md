# Connect transport (`connectx`)

`connectx` is Mindclade's default application RPC adapter. It provides
canonical fault-code mapping, bounded client and handler options, safe error
metadata, request-lineage propagation, authentication and authorization
interceptors, health adapters, and explicitly enabled reflection. Connect
handlers remain wire compatible with Connect, gRPC, and gRPC-Web.

Generated service contracts and handlers remain with their owning API. The
base package does not install telemetry, execute retries, define policy, or
expose internal failure causes. Optional official OpenTelemetry integration
lives in `connectx/otel`.

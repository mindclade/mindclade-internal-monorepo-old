# Native gRPC Transport (`grpcx`)

Native grpc-go construction and adapters for integrations that genuinely need
grpc-go APIs. The package centralizes safe status details, request metadata,
interceptors, TLS/mTLS, health, reflection, bounded message sizes, explicit
retry configuration, and graceful shutdown. It does not register business
services, enable reflection implicitly, terminate the process, or install
process-global telemetry.

Optional official OpenTelemetry stats handlers live in `grpcx/otel`. Connect
remains the preferred application RPC server surface.

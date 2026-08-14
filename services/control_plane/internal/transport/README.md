# Control-plane transport composition

This package is the only supported adapter from repository transport libraries
to the control-plane production bootstrap.

- `HTTP` wraps `httpx.Server` and a pre-bound listener.
- `GRPC` wraps `grpcx.Server` and generated service registrars.
- `MountConnect` mounts generated Connect handlers into the HTTP mux.
- `Bundle` converts configured transports into `bootstrap.Components` and the
  corresponding `servicekit/production` capabilities.

Listener creation, TLS material, generated handlers, repositories, and domain
policy remain in the service-owned composition factory. The adapter does not
open sockets implicitly and rejects empty or inconsistent bundles.

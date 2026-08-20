<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Integration guide](../docs/guides/go-integration-examples.md) · [Validation](../VALIDATION.md)

# Runnable integration examples

> **Maturity:** Implemented local examples using in-memory qualification
> adapters; these are not production deployments.
> **Primary implementation:** Go.

`examples/` contains small, runnable vertical slices that exercise the
implemented Go foundation without requiring external provider credentials.

## Examples

| Example | Demonstrates | Run from the repository root |
| --- | --- | --- |
| [`go/control_plane_api/`](go/control_plane_api/) | Bounded HTTP handling, request lineage, audit, and a transactional-outbox-shaped write boundary | `go run ./examples/go/control_plane_api/cmd/control-plane-api` |
| [`go/event_dispatcher/`](go/event_dispatcher/) | Outbox-to-broker publication through `servicekit/production` | `go run ./examples/go/event_dispatcher` |
| [`go/ingestion_coordinator/`](go/ingestion_coordinator/) | Fenced leadership, leased work, monotonic cursor commit, and transactional outbox | `go run ./examples/go/ingestion_coordinator` |

## Boundary

The memory adapters are deterministic qualification fixtures. Production
composition roots replace them with reviewed PostgreSQL, Pub/Sub, Kubernetes,
GCS, Redis, and other pinned adapters while retaining the same contracts and
lifecycle.

These examples are learning and integration surfaces. Do not copy their
in-memory providers into a production service or interpret a successful local
run as connected-provider qualification.

## Next steps

- Read the [integration examples guide](../docs/guides/go-integration-examples.md).
- Follow the [Go service golden path](../docs/guides/go-service-golden-path.md)
  when creating deployable composition.
- Use [`VALIDATION.md`](../VALIDATION.md) for connected checks and evidence.

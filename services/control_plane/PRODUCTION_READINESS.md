# Control plane production readiness

The reusable Go foundation and role contracts are implemented. The service is
not promoted merely because the scaffold compiles; each role must replace its
fail-closed `UnconfiguredFactory` with a qualified provider factory.

## Required for each role

- [ ] Real PostgreSQL pool and migrations wired through `storage/sql/postgres`
- [ ] Transactional audit, idempotency, and outbox adapters qualified
- [ ] Role-specific coordination loops use fenced claims and bounded queues
- [ ] Domain engines and repositories implemented outside `libs/go`
- [ ] Authentication and authorization provider qualified where required
- [ ] Kubernetes, blob, cache, and transport adapters wired as required
- [ ] Readiness, liveness, drain, cancellation, and shutdown tests pass
- [ ] Failure injection covers database loss, lease loss, duplicate events, and retry exhaustion
- [ ] SLOs, dashboards, alerts, and runbooks linked
- [ ] Bazel build, SBOM, provenance, image signature, and rollback evidence attached
- [ ] `bootstrap.UnconfiguredFactory` absent from the promoted command

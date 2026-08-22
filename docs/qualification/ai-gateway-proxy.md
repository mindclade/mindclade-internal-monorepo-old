# Governed AI Gateway proxy qualification

**Candidate date:** 2026-08-21  
**Owner:** platform-serving

`services/ai_gateway_proxy` is the only proposed provider-egress path for OpenAI-compatible chat
completions, Responses, and embeddings. It is distinct from the model-runtime gateway and from
MLflow's optional metadata mirror.

## Source-qualified behavior

- Google service-account ID tokens are verified against an exact audience, issuer, signature,
  bounded lifetime, and bounded/cached JWKS response before workspace policy lookup.
- Route matching is exact. Native provider passthrough, redirects, split routing, silent fallback,
  streaming, and unqualified guardrails fail closed.
- Every request resolves the current Go bundle, compares route, operation, connection reference,
  policy epoch, pricing version, body limit, reservation ceiling, and observability posture, then
  reserves the server-owned maximum before provider I/O.
- `dispatched` is durable before provider I/O. Measured success commits actual usage; provider
  rejection or unmeasured success is durably max-charged; transport ambiguity remains
  reconciliation-pending for the PostgreSQL sweeper.
- Provider redirects are disabled, response bytes and concurrency are bounded, credentials are
  debug-redacted, payloads are absent from counters/faults, and outbound TLS trusts only the
  externally mounted interception CA through the explicit Secure Web Proxy.

Local tests cover measured commit, provider rejection/max charge, ambiguous transport, and
identity rejection before reservation. Cargo tests and `clippy -D warnings` use the repository's
pinned Rust 1.97.1 toolchain.

## Deployment boundary

The Kubernetes package remains at zero replicas with an invalid image. Activation requires one
connected evidence graph proving the exact caller audience, control-plane identity, approved
bundle digest, provider credential version, Secure Web Proxy/TLS-inspection policy and CA digest,
allowed and denied hostname probes, multi-replica disruption, reconciliation recovery, rate/load
behavior, metrics scraping, alerts, rollback, and empty GitOps drift.

The source does not yet qualify provider streaming, guardrail execution, a real GKE Gateway/IAP
path, a real provider, or a live Secure Web Proxy. Those features stay unavailable; they are not
exceptions that an overlay may enable.

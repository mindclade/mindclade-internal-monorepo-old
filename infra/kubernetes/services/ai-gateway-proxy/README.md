# Governed AI Gateway proxy

This inert Kustomize package is the only provider-egress path for OpenAI-compatible chat,
Responses, and embeddings. The Rust process validates Google service-account ID tokens, resolves
the current effective Go policy against a non-PII digest of the verified caller identity, reserves
the server-owned quota ceiling, records dispatch before provider I/O, and durably max-charges
ambiguous outcomes.

The `control-token` must map to a dedicated service principal with only
`ai_gateway.endpoints.resolve`, `ai_gateway.reservations.create`,
`ai_gateway.reservations.dispatch`, `ai_gateway.reservations.commit`,
`ai_gateway.reservations.reconcile`, `ai_gateway.reservations.release`, and
`ai_gateway.proxy.delegate`. The corresponding control-plane API-key registry and proxy secret
must be reconciled atomically; a generic control-plane key or `ai_gateway.*` grant is not an
acceptable activation input.

Activation requires an attested image digest, two-person-approved endpoint snapshot, external
provider/control credentials, exact client audience, mTLS or equivalent authenticated transport
to the control API, the Terraform-produced TLS-inspecting Secure Web Proxy address plus exact
CONNECT/decrypted-host allowlist and the externally reconciled `ai-gateway-egress-trust` CA,
observed
Gateway/GMP source identities, multi-replica disruption tests, recovery qualification, and SLO
evidence. Repository defaults remain at zero replicas and a non-routable image.

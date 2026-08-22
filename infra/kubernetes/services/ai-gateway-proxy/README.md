# Governed AI Gateway proxy

This inert Kustomize package is the only provider-egress path for OpenAI-compatible chat,
Responses, and embeddings. The Rust process validates Google service-account ID tokens, resolves
the current effective Go policy, reserves the server-owned quota ceiling, records dispatch before
provider I/O, and durably max-charges ambiguous outcomes.

Activation requires an attested image digest, two-person-approved endpoint snapshot, external
provider/control credentials, exact client audience, mTLS or equivalent authenticated transport
to the control API, the Terraform-produced TLS-inspecting Secure Web Proxy address plus exact
CONNECT/decrypted-host allowlist and the externally reconciled `ai-gateway-egress-trust` CA,
observed
Gateway/GMP source identities, multi-replica disruption tests, recovery qualification, and SLO
evidence. Repository defaults remain at zero replicas and a non-routable image.

# Nix cache population

This package defines the reviewed Linux closure that may eventually populate the private
Mindclade Nix cache: `ci-lint`, `ci-terraform`, `ci-infra`, `ci-bazel`, and every exported
`packages.x86_64-linux` derivation. `population.json` is machine-readable and intentionally
sets `activation.enabled` to `false`. The flake asserts the exact locked Attic client source
commit so a nixpkgs update cannot silently change the publication protocol implementation.

`populate.py --plan` is credential-free. `--execute` additionally requires the canonical
protected-main caller, an exact clean `GITHUB_SHA`, native x86_64 Linux, a protected GitHub
environment acknowledgement, a qualified HTTPS Attic endpoint, the cache public key, and a
cache-scoped write token. Pull requests and merge queues cannot publish. Any client-visible
server or Nix signing-key variable is rejected. Attic keeps managed signing server-side; the
publisher receives no signing key and no cache-administration authority. The write token is
stored only in a mode-`0600` ephemeral token file, removed from every child-process environment,
and never placed on a command line. Publication first confirms that the existing cache remains
private and returns the exact reviewed public key.

Activation is a later reviewed change. It must set the contract flag only after the endpoint,
TLS, private read authentication, short-lived scoped tokens, GCS/HMAC behavior, PostgreSQL
backup and restore, cold/warm builds, tamper rejection, and storage/cost limits have connected
evidence. The activation change also adds the protected main caller; this package alone does
not schedule or publish anything. The pinned client and proposed server image currently come
from different upstream commits; connected protocol compatibility is an explicit blocker.

# Protobuf connected qualification

- **Source maturity:** Implemented
- **Environment maturity:** Not yet qualified
- **Authority:** `protocols/compatibility/protobuf-surfaces.json`

The local and hosted presubmit gates compile every promoted schema, verify the
full descriptor surface, build all declared language projections, and exercise
transport policy. They do not claim that a deployed control plane or runtime
has passed authenticated connected qualification.

## Protected release gate

`.github/workflows/protobuf-qualification.yml` is the connected Linux gate. It
runs only for protected `main` in the `protobuf-production-canary` environment;
callers cannot select an endpoint, audience, image digest, WIF provider, or
service account. The environment supplies those values, and GitHub OIDC mints
short-lived Artifact Registry and endpoint credentials.

The implemented gate verifies the digest-pinned contract image's SLSA
provenance, reflects all nine promoted services, requires an anonymous read to
fail with `UNAUTHENTICATED`, performs authenticated read canaries for artifact,
dataset, evaluation, run, and model registry surfaces, stores only response
digests, attests the evidence file, verifies that attestation, and retains the
immutable evidence for 90 days. `canary_test.py` qualifies the fail-closed
command and credential-handling behavior without placing a bearer token in
evidence or command output.

Mutation and streaming behavior remains covered by local transport policy and
compatibility tests until a non-destructive connected fixture is provisioned.
Accordingly the environment maturity above remains **Not yet qualified** until
the protected workflow has produced accepted evidence. Missing credentials,
endpoint, released digest, provenance, or promoted service is a failure, never
a skip.

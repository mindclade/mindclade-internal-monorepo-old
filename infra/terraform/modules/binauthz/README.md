# `binauthz`

Binary Authorization: the point where a signature becomes a precondition for a pod running.

Signing an image and producing an SBOM changes nothing on its own — a cluster that will run
any image gains nothing from a signature existing somewhere. This module is the other half.

## The misconfiguration to know about

**`REQUIRE_ATTESTATION` with an empty attestor list admits every image**, while reading in the
console as the strictest setting available. Both the default rule and each namespace rule
reject that combination in variable validation, rather than leaving it to be discovered during
an incident.

Two more that fail quietly:

- **`cluster` must be exactly `<location>.<cluster_name>`.** Google matches the string
  literally, and a namespace rule whose cluster prefix is wrong applies to nothing while still
  appearing in the policy — which reads as coverage. Passing namespace rules with no `cluster`
  fails a precondition instead of silently doing nothing.
- **`global_policy_evaluation_mode = "DISABLE"`** stops the cluster starting `kube-proxy`, so
  the node never becomes ready. It presents as a networking problem and takes a long time to
  trace back here.

## Keys and who may sign

Each attestor gets a Container Analysis note and an asymmetric SIGN key in
`attestor_key_ring`, HSM-protected by default. The private half never leaves KMS: a pipeline
signs by calling KMS, so a compromised pipeline can mint attestations while it is compromised
but cannot take the key with it. The keys carry `prevent_destroy` — destroying one invalidates
every attestation ever made with it, including those on images currently running.

`attestor_signers` is who may *attest*, deliberately not who may *deploy*. An identity that
can do both approves its own artefacts, which is the control removed. An empty list is
meaningful and is not the same as omitting the entry: it declares that only a human granted
the role out of band may sign, which is how a human-review attestor stays un-automatable.

Every entry in `exempt_images` is a hole by design, so the list is an output as well as an
input — it shows up in a plan diff.

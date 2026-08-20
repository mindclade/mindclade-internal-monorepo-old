# Buildkite identity preflight

This directory contains identity canaries, not an artifact release pipeline. The checked-in
contract is deliberately `unprovisioned`: every live UUID, provider, and service-account value
is `null`, so `wif-preflight.sh` exits before requesting a token. It is safe to merge before the
external Buildkite and normal Google Cloud planes exist, but it is not evidence that either one
is active.

## Fixed pipeline shape

Create three private Buildkite pipelines connected to
`mindclade/mindclade-internal-monorepo` and configure their non-credentialed bootstrap steps to
upload these definitions:

| Pipeline slug | Definition | Authorized step |
| --- | --- | --- |
| `mindclade-artifact-build` | `.buildkite/pipelines/artifact-build.yml` | `artifact-build` |
| `mindclade-artifact-qualify` | `.buildkite/pipelines/artifact-qualify.yml` | `artifact-qualify` |
| `mindclade-artifact-promote` | `.buildkite/pipelines/artifact-promote.yml` | `artifact-promote` |

All jobs target the private `mindclade-artifact-private` queue. The queue must contain only
ephemeral self-hosted Buildkite v4 agents with strict Git commit verification; untrusted
pull-request jobs and production-serving workloads must not share it. Limit pipeline
administration and API build creation to the platform/release groups, and enable Buildkite's
signed-pipeline protections before these identities gain artifact authority.

Buildkite steps have two identifiers with different lifetimes. `key` is the reviewed stable
identifier used by the bootstrap provider (`attribute.step_key`). `BUILDKITE_STEP_ID` and
`BUILDKITE_JOB_ID` are new UUIDs for every run and are evidence fields, not IAM inputs. The
pipeline and organization UUIDs are stable IAM inputs.

## Activation order

1. Create the organization, private cluster/queue, and three pipelines without Google Cloud
   credentials. Record the immutable organization, pipeline, cluster, and queue UUIDs.
2. Apply the normal foundation prerequisites that do not depend on Buildkite. Do not grant an
   agent token or a Google Cloud service-account key to a job.
3. Replace every `null` in `contracts/wif-preflight.json` with the reviewed bootstrap provider,
   three distinct pipeline UUIDs, and three distinct normal-plane service accounts; change
   `activation_state` to `active` in the same protected pull request.
4. Feed the same organization/pipeline pairs into bootstrap, enable Buildkite WIF, and apply
   Ring 0. Its provider requires `pipeline_id` as subject, the separately included
   `organization_id`, the exact step pair, `main`, `self-hosted`, an API/webhook source, and the
   exact provider URL as audience.
5. Apply `infrastructure-live/1-org/automation-iam`. It binds each allowed step to only its own
   service account.
6. Run each pipeline. Its first step has an intentionally untrusted key and passes only when
   Google STS rejects the claims with `invalid_grant` because they fail the provider's attribute
   condition. Network, CLI, and unrelated IAM failures are indeterminate and fail closed. The
   second step passes only when the exact allowed step can mint a short-lived token for its
   dedicated identity. Retain the six credential-free JSON artifacts with the change record.
7. Only after all six observations match may a separate review add build, qualification,
   attestation, registry, or promotion operations.

The helper uses Buildkite's current exact token form:

```text
buildkite-agent oidc request-token
  --audience <exact bootstrap provider URL>
  --subject-claim pipeline_id
  --claim organization_id
  --format gcp
```

`--format gcp` produces a credential-source JSON object, not a complete external-account file.
The helper creates the external-account configuration locally, stores both files under a
mode-0700 temporary directory, disables shell tracing, exchanges the token, and removes the
directory. No OIDC token or access token is uploaded as evidence.

## Source checks

Run the same checks as presubmit:

```bash
nix develop .#ci-infra --command python3 -B .buildkite/scripts/validate_wif_contract.py
nix develop .#ci-infra --command python3 -B -m unittest discover \
  -s .buildkite/tests -p 'test_*.py'
nix develop .#ci-lint --command shellcheck .buildkite/scripts/wif-preflight.sh
```

The validator rejects partial activation, mutable or duplicate IDs, pipeline/queue/step drift,
and privileged operations in the identity canary.

References: [Buildkite OIDC command](https://buildkite.com/docs/agent/cli/reference/oidc) and
[Buildkite immutable job variables](https://buildkite.com/docs/pipelines/configure/environment-variables).

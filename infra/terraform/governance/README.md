# Terraform module-interface governance

This directory owns generated Terraform API documentation and the versioned public
interface contract for every reusable module. Deployable compositions are owned by the
separately controlled live configuration and are deliberately excluded. Manifest schema 2
also fingerprints variable validation conditions, `nullable`/`ephemeral` behavior,
output value expressions, and required-provider source/aliases. It never runs
`terraform plan` or accesses a backend, credentials, state, or a cloud API.

## Protected release source

`.github/workflows/terraform-module-release.yml` consumes an already-created signed annotated
SemVer tag behind the protected `terraform-module-release` environment. It verifies through the
GitHub tag-object API that the tag signature is valid and targets the exact checked commit, then
runs the complete repository-only Terraform gate and attests a deterministic source manifest.
The workflow never creates, moves, or publishes a tag.

Both this policy and the generated interface manifest must already record the matching contract
as `released`; their current `planned` state therefore rejects `v0.4.0` fail closed. A future
review must advance the governed sources without interface drift before an authorized signer may
create that tag. Passing this lane is release provenance for reusable module source, not evidence
that any live composition was planned or applied.

Run the write-side generator only when intentionally changing module documentation or
an interface:

```bash
nix develop .#ci --command infra/terraform/governance/generate.sh
```

Presubmit uses the read-only drift and compatibility check:

```bash
nix develop .#ci --command infra/terraform/governance/check.sh
```

Set `TERRAFORM_INTERFACE_BASE_REF` to a fetched Git revision when comparing a pull
request with its base. If that revision predates the interface manifest, the checker
uses the immutable `v0.1.1` snapshot recorded in `version.toml`.

Generated README content is confined to `BEGIN_TF_DOCS`/`END_TF_DOCS` markers; all
text outside those markers remains handwritten. The committed JSON manifest omits
descriptions but records requirements, input type/default/requiredness/sensitivity,
input validation/nullable/ephemeral behavior, output sensitivity/ephemeral/value-expression
fingerprints, required-provider source/aliases, managed resource addresses, child-module
addresses, and native `moved` mappings.

The current manifest deliberately records the literal `source_revision` value
`working-tree`: generation happens before the resulting commit exists, so inventing a
SHA there would be circular and stale. It is reproducible drift evidence, not release
provenance. Historical baselines alone record an immutable commit SHA and released
contract version.

Breaking changes relative to a released interface require a SemVer-compatible version
increment and a TOML record in `migrations/`. A version whose policy and base manifest are
both `planned` may evolve before publication without manufacturing another version: the
checker authenticates the immutable released fallback and revalidates the entire candidate
and cumulative migration record against it. This exception never applies to a released base,
a version/status mismatch, or a planned-to-released transition that also changes an interface.

The migration record must name every stable detected-change ID, affected module, consumer
step, rollback, and qualification artifact. Address moves additionally need a native Terraform
`moved` block and matching `state_move` table; recording a move cannot waive that requirement.
Every removed managed-resource or child-module address must have exactly one disposition: a
verified state move, or an `intentional_removal` table with a non-empty reason, consumer action,
and rollback action. Moved-mapping removal or retargeting is itself a breaking change.

The base revision must be available locally. An invalid or shallow/unfetched ref is an error,
not permission to compare against older evidence. Fallback to `baselines/v0.1.1.json` occurs
only after Git verifies that the requested base commit either predates the interface manifest
or contains the same still-planned candidate version. The fallback version, source SHA, schema,
status, and local commit identity are checked every time. The checker then archives the Terraform
modules from that exact immutable baseline commit, rebuilds the manifest with the pinned
`terraform-docs`, and requires it to equal the committed fallback byte-for-byte at the decoded
JSON contract level. Valid-looking metadata cannot authenticate forged content.

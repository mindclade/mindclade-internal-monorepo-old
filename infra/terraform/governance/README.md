# Terraform module-interface governance

This directory owns generated Terraform API documentation and the versioned public
interface contract for every reusable module. Manifest schema 2
also fingerprints variable validation conditions, `nullable`/`ephemeral` behavior,
output value expressions, and required-provider source/aliases. It never runs
`terraform plan` or accesses a backend, credentials, state, or a cloud API.

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

Breaking changes require a SemVer-compatible version increment and a TOML record in
`migrations/`. The record must name every stable detected-change ID, affected modules,
consumer steps, rollback, and qualification evidence. Address moves additionally need
a native Terraform `moved` block and matching `state_move` table; recording a move cannot
waive that requirement. Every removed managed-resource or child-module address must have
exactly one disposition: a verified state move, or an `intentional_removal` table with a
non-empty reason, consumer action, and rollback action. Moved-mapping removal or retargeting
is itself a breaking change.

The base revision must be available locally. An invalid or shallow/unfetched ref is an error,
not permission to compare against older evidence. Fallback to `baselines/v0.1.1.json` occurs
only after Git verifies that the requested base commit exists and truly predates the manifest;
the fallback version, source SHA, schema, and local commit identity are checked every time.
The checker then archives the Terraform modules from that exact commit, rebuilds the manifest
with the pinned `terraform-docs`, and requires it to equal the committed fallback byte-for-byte
at the decoded JSON contract level. Valid-looking metadata cannot authenticate forged content.

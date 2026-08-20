# Terraform plan policy

This directory contains the fail-closed Conftest policy for saved Terraform
plan JSON. It evaluates planned values without contacting Google Cloud or a
Terraform backend.

No production policy profile is committed here. `testdata/synthetic-profile.json`
contains invented values for hermetic tests and must not be used as an
environment approval. An approved profile must be created from the applicable
topology, residency, retention, encryption, labeling, and recovery records.

## Stable command

Create one immutable binary plan and derive JSON from that same file:

```sh
terraform plan -out=tfplan
terraform show -json tfplan >plan.json
infra/terraform/policy/check-plan.sh \
  --plan plan.json \
  --profile approved-policy-profile.json
```

The wrapper validates required input collections, computes SHA-256 over the
exact `plan.json` bytes, injects the current UTC evaluation time, writes only
ephemeral Conftest data, and removes it on exit. Plan JSON can contain sensitive
values; store it as access-controlled approval evidence and do not commit it.

The integration contract is:

```text
infra/terraform/policy/check-plan.sh --plan <plan.json> --profile <profile.json> [--approval <approval.json>]
```

Missing files, malformed JSON, or an incomplete/empty profile return nonzero
before evaluation. Policy violations also return nonzero. The public document
contracts are [schemas/profile.schema.json](schemas/profile.schema.json) and
[schemas/approval.schema.json](schemas/approval.schema.json).

## Controls

The policy rejects:

- `allUsers` and `allAuthenticatedUsers`, primitive IAM roles, and managed
  service-account keys;
- all authoritative `*_iam_binding` and `*_iam_policy` resources because they
  can remove grants owned by another state;
- additive IAM member changes with unknown or malformed roles/members; policy
  shape checks additionally keep malformed authoritative resources observable;
- IAM grants without a concrete provider target, and administrative grants
  without an exact, recently issued approval;
- delete and replacement actions without an exact approval bound to the plan;
- data resources with missing labels, unknown classifications, unapproved
  locations, residency violations, insufficient CMEK/retention, or disabled
  provider-native deletion protection.

The governed data types are Cloud Storage, Secret Manager, Pub/Sub, Artifact
Registry, Cloud SQL, and Redis. Pub/Sub does not expose provider-native deletion
protection; its compensating control is the plan-digest-bound destructive-change
gate. Pub/Sub topics must still explicitly supply
`labels["data-classification"]` and all other profile-required labels.

Retention maps to the provider's durable setting: bucket retention seconds,
secret version-destroy delay, topic message retention, Artifact Registry delete
age, Cloud SQL retained/final backups, and Redis RDB snapshot interval.

## Exact approvals

Compute the digest shown in an approval from the same JSON passed to the wrapper:

```sh
python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest())' plan.json
```

Administrative approvals identify the exact Terraform `address`, provider
resource target, `member`, and `role`. Destructive approvals identify the exact
address, resource, and `delete` or `replace` action. Both kinds require a ticket,
owner, reason, UTC `issued_at`, UTC `expires_at`, and plan digest. The issue time
cannot be in the future, expiry must be later than the wrapper-injected current
time, and the full validity window cannot exceed 24 hours. `*`, `?`, `[` and `]`
are forbidden. Public principals, primitive roles, and service-account keys
cannot be approved.

For IAM resources, `resource` is the concrete provider-specific target, such as
project, folder, organization, bucket, service account, Pub/Sub topic/schema/
subscription, secret, repository, KMS key, attestor, analysis note, or IAP
backend service. Unknown targets are never converted to placeholders and cannot
be approved. For deletes and replacements the resource is `before.id`, then
`before.name`, then the Terraform address. The synthetic approvals under
`testdata/` demonstrate both forms.

An approval file is policy input, not proof of authorization by itself. CI must
authenticate its provenance and bind human approval to the saved plan digest.
This wrapper binds approval to the exact plan JSON bytes. Binding the saved
binary plan artifact to the apply operation remains the responsibility of the
external promotion/apply workflow; the JSON digest must not be substituted for
that binary-artifact control.

## Tests

```sh
conftest verify --policy infra/terraform/policy
infra/terraform/policy/test-policy.sh
```

Unit tests exercise OPA helpers, known/unknown IAM values, bounded approval
windows, and Terraform plan nesting. Integration fixtures cover clean and
rejected plans across all six data-resource types, unknown/malformed IAM plans,
exact/expired/long-lived/wildcard/wrong-digest IAM approvals, unresolved IAM
targets, Pub/Sub destructive approvals, and fail-closed empty/fractional
profiles. The nested fixture shapes were checked
against the pinned Google provider schema: SQL settings, Secret Manager
replication, and Artifact Registry cleanup conditions are list-backed plan JSON
(cleanup policies themselves are serialized from a provider set as an array).

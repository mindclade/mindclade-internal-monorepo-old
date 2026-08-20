# `scc`

Security Command Center: where findings go, and what is knowingly ignored.

Three things are kept separate here, because collapsing any two produces a control that looks
present and is not:

- **`notifications`** — one machine-readable feed and one urgent channel, on *different*
  topics. A stream nobody watches and a page nobody can ignore are different things, and
  conflating them produces a channel people mute within a month.
- **`bigquery_export`** — findings next to the audit logs, so they can be joined. The join
  that matters: a finding says a service account has excessive permissions, and the audit log
  says whether it ever used them. The dataset sets `delete_contents_on_destroy = false`, so an
  attacker reaching the export identity cannot erase it.
- **`mute_configs`** — muting in the console leaves no reason, no owner, and no expiry.
  Declared here, a mute is a pull request somebody reviews, and one that shows up in a diff
  when it is still present a year later. Descriptions under 30 characters are rejected: a mute
  with no reason is a finding somebody found inconvenient. Every mute is dynamic and must
  carry an accountable owner and future RFC3339 expiry; an expired mute fails planning until
  it is removed or deliberately renewed.

Notification topics and the findings dataset require explicit CMEK names. `project_number`
derives the documented Pub/Sub and BigQuery encryption service identities, and
`required_kms_grants` hands the exact additive grants to the key-owning state. This module
does not mutate KMS IAM. BigQuery CMEK location compatibility is checked for `US`, `EU`, and
regional datasets; live qualification must still prove service-identity creation and key
availability before enabling the export.

## Detector enablement is not managed here, and cannot be

**There is no Terraform resource for enabling a built-in SCC service** in either the `google`
or `google-beta` provider. The provider exposes custom modules, sources, notification configs,
exports, and mutes — and nothing that turns Security Health Analytics or Container Threat
Detection on.

`services` is therefore accepted, validated, and returned as `service_enablement_commands`
rather than quietly dropped. Two things this deliberately avoids:

- a `local-exec` calling gcloud — it would run on whichever machine happened to apply, with
  that operator's credentials, and record nothing in state, so a drift check could not tell an
  enabled detector from one somebody turned off in the console;
- a `null_resource` that pretends — a green apply asserting a detector is on when nothing
  checked is the exact failure this module exists to avoid.

Run the commands once, then let the drift sweep compare `gcloud scc manage services list`
against the `enabled_services` output.

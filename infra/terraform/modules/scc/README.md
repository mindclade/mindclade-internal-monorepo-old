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

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.9.0, < 2.0.0 |
| <a name="requirement_google"></a> [google](#requirement\_google) | >= 7.41.0, < 8.0.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_google"></a> [google](#provider\_google) | >= 7.41.0, < 8.0.0 |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_bigquery_export"></a> [bigquery\_export](#input\_bigquery\_export) | Continuous export of findings into BigQuery, so a finding can be joined against the audit<br/>dataset. The join that matters: a finding says a service account has excessive<br/>permissions, and the audit log says whether it used them. | <pre>object({<br/>    dataset_id   = string<br/>    location     = optional(string)<br/>    kms_key_name = string<br/>    filter       = string<br/>  })</pre> | `null` | no |
| <a name="input_labels"></a> [labels](#input\_labels) | Labels applied to the Pub/Sub topics and BigQuery dataset. | `map(string)` | `{}` | no |
| <a name="input_location"></a> [location](#input\_location) | Location for the BigQuery findings dataset | `string` | `"US"` | no |
| <a name="input_mute_configs"></a> [mute\_configs](#input\_mute\_configs) | Standing mutes, keyed by short name.<br/><br/>Muting in the UI leaves no reason, no owner, and no expiry. Declared here, a mute is a<br/>pull request somebody reviews — and one that shows up in a diff when it is still present<br/>a year later. | <pre>map(object({<br/>    description = string<br/>    filter      = string<br/>    owner       = string<br/>    expiry_time = string<br/>  }))</pre> | `{}` | no |
| <a name="input_notifications"></a> [notifications](#input\_notifications) | Notification configs keyed by short name. Each creates its own Pub/Sub topic.<br/><br/>Separate destinations are the point: a stream nobody watches and a page nobody can ignore<br/>are different things, and conflating them produces a channel people mute. | <pre>map(object({<br/>    description = string<br/>    filter      = string<br/>    pubsub_topic = object({<br/>      name         = string<br/>      kms_key_name = string<br/>    })<br/>  }))</pre> | `{}` | no |
| <a name="input_org_id"></a> [org\_id](#input\_org\_id) | Numeric organization id Security Command Center is configured on | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project holding the notification topics and the BigQuery export | `string` | n/a | yes |
| <a name="input_project_number"></a> [project\_number](#input\_project\_number) | Numeric number of project\_id, used to derive Google-managed CMEK service identities. | `string` | n/a | yes |
| <a name="input_services"></a> [services](#input\_services) | Built-in SCC services keyed by service name, each ENABLE or DISABLE.<br/><br/>Set explicitly rather than left at the tier default: the default changes with the tier,<br/>and a silent downgrade is indistinguishable from "no findings". | `map(string)` | `{}` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_disabled_services"></a> [disabled\_services](#output\_disabled\_services) | Detectors explicitly turned off. A short list that should stay short. |
| <a name="output_enabled_services"></a> [enabled\_services](#output\_enabled\_services) | Detectors explicitly enabled. Surfaced so a reviewer can diff what is running against what was last approved, rather than reading a tier default that moves. |
| <a name="output_findings_dataset_id"></a> [findings\_dataset\_id](#output\_findings\_dataset\_id) | BigQuery dataset holding exported findings, or null when no export is configured. |
| <a name="output_mute_config_ids"></a> [mute\_config\_ids](#output\_mute\_config\_ids) | Standing mute ids keyed by name. |
| <a name="output_mute_governance"></a> [mute\_governance](#output\_mute\_governance) | Owner and mandatory expiry for each standing dynamic mute. |
| <a name="output_mute_reasons"></a> [mute\_reasons](#output\_mute\_reasons) | Why each mute exists, keyed by name. In state and in the plan diff, so a mute that is still present a year later is visible without opening the console. |
| <a name="output_notification_config_ids"></a> [notification\_config\_ids](#output\_notification\_config\_ids) | SCC notification config resource ids keyed by name. |
| <a name="output_notification_topic_ids"></a> [notification\_topic\_ids](#output\_notification\_topic\_ids) | Pub/Sub topic ids keyed by notification name. Anything wanting findings subscribes to one of these rather than polling SCC. |
| <a name="output_required_kms_grants"></a> [required\_kms\_grants](#output\_required\_kms\_grants) | Additive grants the key-owning state must apply before creating CMEK-protected findings destinations. |
| <a name="output_service_enablement_commands"></a> [service\_enablement\_commands](#output\_service\_enablement\_commands) | The gcloud invocations that actually enable or disable each detector.<br/><br/>No Terraform resource exists for built-in SCC service enablement, so this is the honest<br/>interface: the module records what was intended and hands back the commands, rather than<br/>reporting a green apply for something it never configured. Run these once, then let the<br/>drift sweep compare `gcloud scc manage services list` against `enabled_services`. |
<!-- END_TF_DOCS -->

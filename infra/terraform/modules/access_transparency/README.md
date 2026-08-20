# `access_transparency`

The record of Google personnel reading customer content, and the alert that makes somebody
look at it.

Both halves are here because either alone is close to useless. A bucket nobody reads is
evidence for an investigation that may never start; an alert with no durable record is a
notification somebody dismissed. Together they answer "did this happen, and was it justified"
months later, which is when the question actually gets asked.

Enabling Access Transparency itself is an organization-level entitlement tied to a support
plan. It is not a Terraform resource and is not attempted here — this module handles what
happens to the logs once the entitlement exists.

## Why each guard is there

- **The filter must mention `access_transparency`.** A filter that selects nothing produces an
  empty bucket, and an empty bucket is indistinguishable from an estate nobody accessed —
  exactly the wrong conclusion to draw silently.
- **Retention is at least a year**, enforced by a bucket retention policy rather than a
  lifecycle rule. A lifecycle rule deletes on a schedule; a retention policy stops an object
  being deleted early, *including by whoever is being investigated*.
- **The archive cannot be force-destroyed.** Provider and Terraform deletion guards plus a
  90-day soft-delete window protect the container and recover accidentally deleted objects;
  the retention policy remains the stronger minimum-age control.
- **The archive requires CMEK.** `encryption_key` is a full Cloud KMS CryptoKey resource name
  in the bucket location; its owning state grants the Cloud Storage service agent
  encrypter/decrypter access before this module is rolled out.
- **Archive access is logged elsewhere.** `access_log_bucket_name` must identify a distinct,
  separately governed bucket. `required_access_log_writer_grant` reports the additive
  Storage analytics group grant its owning state must apply; keep both buckets in compatible
  locations, organizations, and VPC Service Controls perimeters.
- **An alert with no notification channels is rejected.** Google accepts one, the policy shows
  as enabled, and the first anyone knows is that a page never arrived.
- **The alert is a log-match condition, not a metric threshold.** A metric needs an aggregation
  window, and any window turns "somebody read customer data" into a count — which is the one
  framing that makes the individual justification unreadable. The rate limit is set to the
  shortest period the API permits, present to satisfy the API rather than to suppress
  anything.

`notification_channels` takes email addresses; a Monitoring channel is created for each.

The `writer_identity` output is what to check against the bucket's IAM policy if entries stop
arriving — a sink whose writer lacks permission reports healthy and writes nothing.

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
| <a name="input_alert"></a> [alert](#input\_alert) | Alert raised on every access.<br/><br/>Not a threshold and not a daily digest. The volume justifies it — this fires a handful of<br/>times a year — and the value of the record is entirely in someone reading it while the<br/>support case that prompted it is still open.<br/><br/>`notification_channels` takes email addresses; a channel is created for each. | <pre>object({<br/>    display_name          = string<br/>    severity              = optional(string, "WARNING")<br/>    filter                = string<br/>    notification_channels = optional(list(string), [])<br/>    documentation         = optional(string, "")<br/>  })</pre> | `null` | no |
| <a name="input_labels"></a> [labels](#input\_labels) | Labels applied to the archive bucket. | `map(string)` | `{}` | no |
| <a name="input_org_id"></a> [org\_id](#input\_org\_id) | Numeric organization id whose Access Transparency logs are exported | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project holding the archive bucket, the notification channels, and the alert policy | `string` | n/a | yes |
| <a name="input_sink"></a> [sink](#input\_sink) | Where Access Transparency entries are kept.<br/><br/>These are low-volume — tens of entries a year on a healthy estate — so retention costs<br/>almost nothing, and the alternative is discovering the window was too short during the<br/>investigation that needed it. | <pre>object({<br/>    name        = string<br/>    destination = optional(string, "storage")<br/>    filter      = string<br/>    bucket = object({<br/>      name                     = string<br/>      location                 = string<br/>      access_log_bucket_name   = string<br/>      access_log_object_prefix = optional(string, "access-transparency/")<br/>      encryption_key           = string<br/>      retention_days           = optional(number, 2555)<br/>    })<br/>  })</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_alert_policy_id"></a> [alert\_policy\_id](#output\_alert\_policy\_id) | Alert policy resource id, or null when no alert is configured. |
| <a name="output_bucket_name"></a> [bucket\_name](#output\_bucket\_name) | Archive bucket holding the Access Transparency records. |
| <a name="output_notification_channel_ids"></a> [notification\_channel\_ids](#output\_notification\_channel\_ids) | Monitoring notification channel ids keyed by email address. |
| <a name="output_required_access_log_writer_grant"></a> [required\_access\_log\_writer\_grant](#output\_required\_access\_log\_writer\_grant) | Additive grant required on the separately governed access-log bucket. |
| <a name="output_retention_days"></a> [retention\_days](#output\_retention\_days) | How long a record cannot be deleted for, in days. |
| <a name="output_sink_id"></a> [sink\_id](#output\_sink\_id) | Organization sink resource id. |
| <a name="output_writer_identity"></a> [writer\_identity](#output\_writer\_identity) | Service account the sink writes as. Check this against the bucket's IAM policy when entries stop arriving — a sink whose writer lacks permission reports healthy and delivers nothing. |
<!-- END_TF_DOCS -->

# VPC firewall module

Creates classic VPC firewall rules against existing networks without claiming network ownership.
Rules are grouped by an environment or ownership key and validate direction, action, ranges, and
mutually exclusive allow/deny blocks before provider evaluation.

The live estate uses explicit high-priority allows and a logged priority-65000 egress deny. The
module never inserts an implicit internet allow and can automatically enable metadata-rich logging
only for deny rules.

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
| <a name="input_firewalls"></a> [firewalls](#input\_firewalls) | Classic VPC firewall policies keyed by environment or another stable owner. | <pre>map(object({<br/>    project_id                  = string<br/>    network                     = string<br/>    enable_logging_on_deny_only = optional(bool, true)<br/>    rules = map(object({<br/>      direction          = string<br/>      priority           = number<br/>      action             = string<br/>      description        = optional(string, "Managed VPC firewall rule.")<br/>      disabled           = optional(bool, false)<br/>      source_ranges      = optional(set(string), [])<br/>      destination_ranges = optional(set(string), [])<br/>      source_tags        = optional(set(string), [])<br/>      target_tags        = optional(set(string), [])<br/>      allow = optional(list(object({<br/>        protocol = string<br/>        ports    = optional(set(string), [])<br/>      })), [])<br/>      deny = optional(list(object({<br/>        protocol = string<br/>        ports    = optional(set(string), [])<br/>      })), [])<br/>      log_config = optional(object({<br/>        metadata = optional(string, "INCLUDE_ALL_METADATA")<br/>      }))<br/>    }))<br/>  }))</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_rule_ids"></a> [rule\_ids](#output\_rule\_ids) | Firewall rule IDs keyed by owner and rule name. |
| <a name="output_rule_names"></a> [rule\_names](#output\_rule\_names) | Firewall rule names keyed by owner and rule name. |
<!-- END_TF_DOCS -->

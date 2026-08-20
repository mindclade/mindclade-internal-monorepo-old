# Cloud SQL for PostgreSQL module

This module creates a regional, private-IP PostgreSQL primary with connector
enforcement, trusted client certificates, disk growth bounds, Query Insights, automated
backups, point-in-time recovery, retained backups, a primary final deletion backup, and
two layers of deletion protection. It can create databases, passwordless Cloud
IAM database principals, and optional cross-region read replicas.

The accepted service profile is deliberately narrow: PostgreSQL 17, Enterprise
edition, an Enterprise `db-custom` tier, and `PD_SSD`. This prevents callers from
combining independently valid edition, machine-series, and disk values into an
API-invalid plan; any Enterprise Plus, Hyperdisk, or major-version expansion requires
separate migration, performance, backup, and restore qualification.

Additional reviewed database flags, including IAM database authentication by
default, are applied explicitly to both the primary and every replica. Replicas must
use the primary VPC. A CMEK primary requires a separate full CryptoKey resource name
in each replica region; a Google-managed-encryption primary forbids an inconsistent
replica CMEK. Key IAM remains outside this state boundary.

The module never creates a database password. It assumes Private Service Access,
service APIs, KMS service-agent permissions, client identity, DNS, firewall policy,
monitoring notification channels, and a protected remote Terraform backend already
exist. Replica promotion, restore, traffic shift, and failback remain approval-gated
runbook operations. Primary disk size is ignored after service-side autoresize growth
so a later plan cannot attempt an unsupported shrink.

Mandatory database flags log connections, disconnections, hostnames, checkpoints,
lock waits, temporary files, error statements, and DDL, and enable the Cloud SQL pgAudit
integration. Temporary-file logging is set to zero so every spill is recorded; size and
retention controls belong in the central logging boundary because these entries can expose
query and workload characteristics.
Create and configure the `pgaudit` extension through a reviewed database migration;
the instance flag alone does not install it inside each database. Route database logs
to the approved central log project with access controls and retention appropriate for
potentially sensitive query metadata.

```hcl
module "postgres" {
  source = "../../modules/postgres"

  project_id         = "mindclade-production"
  name               = "mindclade-control-plane"
  region             = "us-central1"
  private_network    = "projects/mindclade-host/global/networks/production"
  allocated_ip_range = "google-managed-services-production"
  backup_location    = "us"
  environment        = "production"
  owner              = "cloud-platform"
}
```

Before rollout, verify edition/tier compatibility, quotas, maintenance timing,
private connectivity, application connector support, data-governance approval for
Query Insights, backup location, replica lag alerting, restore behavior, achieved
RPO/RTO, and failback through an isolated exercise. Offline tests prove HCL policy
only; they do not prove a recoverable live service.

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
| <a name="input_allocated_ip_range"></a> [allocated\_ip\_range](#input\_allocated\_ip\_range) | Optional private service access allocated range name | `string` | `null` | no |
| <a name="input_backup_location"></a> [backup\_location](#input\_backup\_location) | Explicit backup region or multi-region chosen for the recovery design | `string` | n/a | yes |
| <a name="input_backup_start_time_utc"></a> [backup\_start\_time\_utc](#input\_backup\_start\_time\_utc) | Daily backup start in HH:MM UTC | `string` | `"05:00"` | no |
| <a name="input_connector_enforcement"></a> [connector\_enforcement](#input\_connector\_enforcement) | Require Cloud SQL connectors instead of direct database connections | `string` | `"REQUIRED"` | no |
| <a name="input_data_classification"></a> [data\_classification](#input\_data\_classification) | Data-classification governance label | `string` | `"confidential"` | no |
| <a name="input_database_flags"></a> [database\_flags](#input\_database\_flags) | Additional reviewed PostgreSQL flags; mandatory audit flags cannot be overridden | `map(string)` | <pre>{<br/>  "cloudsql.iam_authentication": "on"<br/>}</pre> | no |
| <a name="input_database_version"></a> [database\_version](#input\_database\_version) | Repository-qualified PostgreSQL version | `string` | `"POSTGRES_17"` | no |
| <a name="input_databases"></a> [databases](#input\_databases) | Databases to create without credentials | <pre>map(object({<br/>    charset   = optional(string, "UTF8")<br/>    collation = optional(string, "en_US.UTF8")<br/>  }))</pre> | `{}` | no |
| <a name="input_disk_autoresize_limit_gb"></a> [disk\_autoresize\_limit\_gb](#input\_disk\_autoresize\_limit\_gb) | Storage growth cap; zero uses the service maximum and requires cost monitoring | `number` | `1000` | no |
| <a name="input_disk_size_gb"></a> [disk\_size\_gb](#input\_disk\_size\_gb) | Initial primary disk size in GiB | `number` | `100` | no |
| <a name="input_disk_type"></a> [disk\_type](#input\_disk\_type) | Primary and replica disk type | `string` | `"PD_SSD"` | no |
| <a name="input_edition"></a> [edition](#input\_edition) | Repository-qualified Cloud SQL edition | `string` | `"ENTERPRISE"` | no |
| <a name="input_enable_private_google_access"></a> [enable\_private\_google\_access](#input\_enable\_private\_google\_access) | Allow supported Google services to reach the instance through private paths | `bool` | `true` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment governance label | `string` | n/a | yes |
| <a name="input_final_backup_retention_days"></a> [final\_backup\_retention\_days](#input\_final\_backup\_retention\_days) | Retention for the final backup after an approved deletion | `number` | `30` | no |
| <a name="input_iam_database_users"></a> [iam\_database\_users](#input\_iam\_database\_users) | Passwordless Cloud IAM database principals; names must follow Cloud SQL PostgreSQL conventions | <pre>set(object({<br/>    name = string<br/>    type = string<br/>  }))</pre> | `[]` | no |
| <a name="input_kms_key_name"></a> [kms\_key\_name](#input\_kms\_key\_name) | Optional regional CryptoKey for the primary; grant the Cloud SQL service agent separately | `string` | `null` | no |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional labels; baseline governance labels take precedence | `map(string)` | `{}` | no |
| <a name="input_maintenance_day"></a> [maintenance\_day](#input\_maintenance\_day) | Maintenance weekday numbered 1 (Monday) through 7 (Sunday) | `number` | `7` | no |
| <a name="input_maintenance_hour_utc"></a> [maintenance\_hour\_utc](#input\_maintenance\_hour\_utc) | Maintenance start hour in UTC | `number` | `7` | no |
| <a name="input_maintenance_update_track"></a> [maintenance\_update\_track](#input\_maintenance\_update\_track) | Service update rollout track | `string` | `"stable"` | no |
| <a name="input_name"></a> [name](#input\_name) | Primary instance name | `string` | n/a | yes |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team governance label | `string` | n/a | yes |
| <a name="input_private_network"></a> [private\_network](#input\_private\_network) | VPC resource name used for private IP | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project that owns the instance | `string` | n/a | yes |
| <a name="input_query_insights_enabled"></a> [query\_insights\_enabled](#input\_query\_insights\_enabled) | Enable Query Insights; data-governance owners must approve query text retention | `bool` | `true` | no |
| <a name="input_query_plans_per_minute"></a> [query\_plans\_per\_minute](#input\_query\_plans\_per\_minute) | Query execution plans captured per minute | `number` | `5` | no |
| <a name="input_query_string_length"></a> [query\_string\_length](#input\_query\_string\_length) | Maximum query text length retained by Query Insights | `number` | `1024` | no |
| <a name="input_read_replicas"></a> [read\_replicas](#input\_read\_replicas) | Optional cross-region read replicas; promotion and failback remain runbook-controlled | <pre>map(object({<br/>    region             = string<br/>    tier               = string<br/>    private_network    = string<br/>    allocated_ip_range = optional(string)<br/>    kms_key_name       = optional(string)<br/>  }))</pre> | `{}` | no |
| <a name="input_region"></a> [region](#input\_region) | Primary instance region | `string` | n/a | yes |
| <a name="input_retained_backups"></a> [retained\_backups](#input\_retained\_backups) | Number of automated backups retained | `number` | `14` | no |
| <a name="input_tier"></a> [tier](#input\_tier) | Qualified Enterprise db-custom primary machine tier | `string` | `"db-custom-2-7680"` | no |
| <a name="input_transaction_log_retention_days"></a> [transaction\_log\_retention\_days](#input\_transaction\_log\_retention\_days) | PITR transaction-log retention | `number` | `7` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_databases"></a> [databases](#output\_databases) | Created database names |
| <a name="output_primary"></a> [primary](#output\_primary) | Primary instance connection metadata |
| <a name="output_read_replicas"></a> [read\_replicas](#output\_read\_replicas) | Replica connection metadata |
<!-- END_TF_DOCS -->

# Memorystore for Redis module

This module provisions a regional `STANDARD_HA` Memorystore instance on an
existing VPC through Private Service Access. Authentication, server-authenticated
TLS, RDB persistence, a reviewed maintenance window, deletion protection, and
governance labels are mandatory.

The module does not create the VPC, allocated range, service networking
connection, KMS IAM grant, client firewall policy, or a secret containing the
generated auth string. Those belong to independently owned states. Terraform
state contains service-generated sensitive values and must use a protected remote
backend.

```hcl
module "cache" {
  source = "../../modules/redis"

  project_id         = "mindclade-production"
  name               = "control-plane-cache"
  region             = "us-central1"
  authorized_network = "projects/mindclade-host/global/networks/production"
  reserved_ip_range  = "google-managed-services-production"
  environment        = "production"
  owner              = "cloud-platform"
}
```

Offline validation cannot prove service networking, failover, TLS clients,
capacity, monitoring, backup recovery, or live IAM. Exercise those before rollout.

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
| <a name="input_alternative_zone"></a> [alternative\_zone](#input\_alternative\_zone) | Optional failover zone; must differ from primary\_zone | `string` | `null` | no |
| <a name="input_authorized_network"></a> [authorized\_network](#input\_authorized\_network) | Full self-link of the VPC allowed to connect | `string` | n/a | yes |
| <a name="input_data_classification"></a> [data\_classification](#input\_data\_classification) | Data-classification governance label | `string` | `"confidential"` | no |
| <a name="input_display_name"></a> [display\_name](#input\_display\_name) | Human-readable instance name | `string` | `"Mindclade Redis"` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment governance label | `string` | n/a | yes |
| <a name="input_kms_key_name"></a> [kms\_key\_name](#input\_kms\_key\_name) | Optional CryptoKey resource name; grant the Redis service agent access separately | `string` | `null` | no |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional labels; baseline governance labels take precedence | `map(string)` | `{}` | no |
| <a name="input_maintenance_day"></a> [maintenance\_day](#input\_maintenance\_day) | Weekly maintenance day | `string` | `"SUNDAY"` | no |
| <a name="input_maintenance_hour_utc"></a> [maintenance\_hour\_utc](#input\_maintenance\_hour\_utc) | Start hour for weekly maintenance in UTC | `number` | `7` | no |
| <a name="input_memory_size_gb"></a> [memory\_size\_gb](#input\_memory\_size\_gb) | Provisioned memory in GiB | `number` | `5` | no |
| <a name="input_name"></a> [name](#input\_name) | Redis instance name | `string` | n/a | yes |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team governance label | `string` | n/a | yes |
| <a name="input_primary_zone"></a> [primary\_zone](#input\_primary\_zone) | Optional primary zone; null lets the service choose | `string` | `null` | no |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project that owns the Redis instance | `string` | n/a | yes |
| <a name="input_rdb_snapshot_period"></a> [rdb\_snapshot\_period](#input\_rdb\_snapshot\_period) | RDB persistence frequency | `string` | `"SIX_HOURS"` | no |
| <a name="input_rdb_snapshot_start_time"></a> [rdb\_snapshot\_start\_time](#input\_rdb\_snapshot\_start\_time) | Optional RFC3339 time anchoring the RDB schedule | `string` | `null` | no |
| <a name="input_redis_configs"></a> [redis\_configs](#input\_redis\_configs) | Runtime Redis configuration | `map(string)` | <pre>{<br/>  "maxmemory-policy": "allkeys-lru"<br/>}</pre> | no |
| <a name="input_redis_version"></a> [redis\_version](#input\_redis\_version) | Memorystore Redis major/minor version | `string` | `"REDIS_7_2"` | no |
| <a name="input_region"></a> [region](#input\_region) | Region for the instance | `string` | n/a | yes |
| <a name="input_reserved_ip_range"></a> [reserved\_ip\_range](#input\_reserved\_ip\_range) | Name of the private service access allocated range | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_instance"></a> [instance](#output\_instance) | Non-secret connection metadata; use TLS and retrieve auth through a protected runtime path |
| <a name="output_server_ca_certs"></a> [server\_ca\_certs](#output\_server\_ca\_certs) | Server CA certificates for TLS client trust configuration |
<!-- END_TF_DOCS -->

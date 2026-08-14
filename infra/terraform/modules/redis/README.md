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

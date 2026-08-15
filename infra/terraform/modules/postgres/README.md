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
lock waits, error statements, and DDL, and enable the Cloud SQL pgAudit integration.
Create and configure the `pgaudit` extension through a reviewed database migration;
the instance flag alone does not install it inside each database. Route database logs
to the approved central log project with access controls and retention appropriate for
potentially sensitive query metadata.

```hcl
module "postgres" {
  source = "../../modules/cloud-sql-postgres"

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

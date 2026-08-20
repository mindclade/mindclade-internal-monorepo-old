# Immutable audit archive composition

This module composes `../log_sink` into one central organization- or folder-level Cloud
Audit Logs archive. It captures Admin Activity, System Event, Policy Denied, and Data
Access log IDs from the parent and all descendants, with no exclusions or caller-supplied
narrowing filter.

The GCS destination enforces uniform access, public-access prevention, versioning,
90-day soft delete by default, CMEK, `force_destroy = false`, provider deletion policy,
Terraform `prevent_destroy`, and a locked retention policy. Storage-class transitions
reduce long-term cost but never delete audit objects.

Bucket Lock is irreversible: retention cannot later be shortened or removed, and changing
location requires a new bucket. The caller must supply the exact confirmation only after
security, legal, recovery, and cost owners approve the name, location, key, and retention.
A migration must overlap old and new sinks and verify delivery before retiring anything.

This module routes logs that Google Cloud emits; it does not enable Data Access audit logs
for individual services. Manage those audit configs at the organization/folder/project IAM
authority and verify expected log types with canary activity. It also cannot prove delivery:
alert on sink errors, periodically compare expected versus archived entries, verify the
unique writer's object-creator grant, and test evidence retrieval.

`destination_project_default_retention_days` affects only the central destination
project's own `_Default` bucket. It cannot change every descendant project's local bucket
and defaults to zero (unmanaged).

```hcl
module "audit_archive" {
  source = "../../modules/audit_archive"

  parent      = "organizations/123456789012"
  project_id  = "mindclade-audit"
  environment                 = "production"
  owner                       = "security"
  storage_service_agent_email = "service-123456789012@gs-project-accounts.iam.gserviceaccount.com"
  bucket_name                 = "mindclade-central-audit-archive"
  location                    = "US"
  kms_key_name                = "projects/mindclade-security/locations/us/keyRings/audit/cryptoKeys/archive"

  retention_lock_confirmation = "LOCKING A CLOUD STORAGE RETENTION POLICY IS IRREVERSIBLE"
}
```

The Cloud Storage service agent needs access to the archive CMEK. The required additive
grant is exported, while Key IAM remains in the key-owning state; verify that grant in the
live project before apply.
Mock-provider tests do not prove audit-config enablement, KMS compatibility, VPC Service
Controls behavior, log delivery, retention-law suitability, or evidence recovery.

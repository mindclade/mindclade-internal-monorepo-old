# Aggregated log sink module

This module creates aggregated sinks on either an organization or a folder and routes each
sink to a dedicated Cloud Logging or GCS bucket in `project_id`. Organization and folder
sinks use their respective provider resources; a `folders/...` parent is never passed as
an organization ID.

Every sink requests a unique writer identity. The module then adds only the destination
grant that identity needs: conditional Logging bucket-writer for a Cloud Logging
destination or GCS object-creator for an archive. A configured sink is not proof of
delivery, so monitor sink errors and verify expected canary entries.

Cloud Logging destinations have bounded retention, configurable location, deletion policy,
and Terraform destruction protection. Log Analytics is a creation-time decision. GCS
destinations enforce uniform access, public-access prevention, versioning, soft delete,
`force_destroy = false`, provider deletion policy, and Terraform destruction protection.
CMEK is optional at this generic layer; the key-owning state must grant the relevant
service agent.

GCS retention is distinct from lifecycle deletion. Retention locking is irreversible and
requires both `lock_retention_policy = true` and the exact
`retention_lock_confirmation`. Generic archives default to an unlocked policy; the
`audit_archive` composition requires a locked policy.

`default_sink_retention_days` manages only the destination project's own global
`_Default` bucket. It cannot update the local buckets of descendant projects. The
backward-compatible default is 30 days; set it to zero to leave that bucket unmanaged.

```hcl
module "logs" {
  source = "../../modules/log_sink"

  parent     = "folders/123456789012"
  project_id = "mindclade-logging"

  sinks = {
    application-hot = {
      description      = "Queryable application logs."
      destination      = "logging"
      filter           = "resource.type=\"k8s_container\""
      retention_days   = 30
      enable_analytics = true
    }
  }
}
```

Mock-provider tests validate resource selection and configuration only. They do not prove
Logging API enablement, IAM propagation, CMEK access, destination compatibility, live
delivery, exclusions, retention execution, cost, or recovery.

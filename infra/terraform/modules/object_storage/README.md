# Object storage composition

This is an opinionated multi-bucket composition of `../storage`; it does not declare a
second bucket implementation. The sibling storage module remains the single authority for
bucket resources, deletion protection, uniform access, public-access prevention,
versioning, soft delete, server-access logging, retention, lifecycle, CMEK, and additive
object IAM.

The composition creates three explicit trust classes:

- one restricted access-log bucket, retained for at least a year and transitioned through
  colder storage without lifecycle deletion;
- mutable governed data buckets classified as raw, curated, reference, dataset, or
  evidence;
- restricted create-only AI artifact buckets classified as checkpoint, model, evaluation,
  or release evidence.

AI publishers receive creator plus viewer, never object-admin, and the underlying module
permits only incomplete-multipart cleanup. Clients must still upload with
`ifGenerationMatch=0`, verify checksums/digests, and publish a digest-bound manifest last.
Bucket controls cannot enforce that application protocol.

All managed buckets require CMEK and are protected with provider deletion policy plus
Terraform `prevent_destroy`. KMS IAM stays in the key-owning state; the composition
exports the required Cloud Storage service-agent grants. The managed access-log
bucket grants only object-creator to Cloud Storage's analytics writer. It must itself log to
a separately governed upstream bucket to avoid a self-logging cycle; the required upstream
grant is exported. Before apply, verify that source and logging buckets meet Cloud Storage
location, organization, and VPC Service Controls requirements.

```hcl
module "object_storage" {
  source = "../../modules/object_storage"

  project_id                     = "mindclade-production"
  environment                    = "production"
  owner                          = "data-platform"
  storage_service_agent_email    = "service-123456789012@gs-project-accounts.iam.gserviceaccount.com"
  upstream_access_log_bucket_name = "mindclade-central-storage-access-logs"

  access_log_bucket = {
    name         = "mindclade-production-storage-access"
    location     = "US"
    kms_key_name = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
  }

  data_buckets = {
    curated = {
      name          = "mindclade-production-curated"
      location      = "US"
      kms_key_name  = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
      data_class    = "curated"
      readers       = ["group:data-consumers@example.com"]
      writers       = ["serviceAccount:curator@mindclade-production.iam.gserviceaccount.com"]
    }
  }

  ai_artifact_buckets = {
    checkpoints = {
      name           = "mindclade-production-checkpoints"
      location       = "US"
      kms_key_name   = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
      artifact_class = "checkpoint"
      publishers     = ["serviceAccount:trainer@mindclade-production.iam.gserviceaccount.com"]
      readers        = ["serviceAccount:runtime@mindclade-production.iam.gserviceaccount.com"]
    }
  }
}
```

Changing a bucket key or name can imply replacement, while deletion guards intentionally
stop the operation. Use an approved migration with copied-and-verified data and a
state-safe address move; do not disable the guards casually. Mock-provider tests do not
prove KMS grants, access-log delivery, object recovery, legal holds, lifecycle execution,
or restore objectives in a live project.

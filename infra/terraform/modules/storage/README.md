# Cloud Storage module

This module creates one private, deletion-protected bucket. Uniform bucket-level
access, public-access prevention, versioning, and a 30-day soft-delete window are
secure defaults. IAM grants are additive and object-scoped; public principals are
rejected. Every bucket configures a separately governed server-access-log destination.
The log-bucket-owning state must grant `roles/storage.objectCreator` to
`group:cloud-storage-analytics@google.com`; the module exports that required grant but
does not take ownership of a shared bucket from each source-bucket state. Before apply,
verify that both buckets satisfy Cloud Storage's location, organization, and VPC
Service Controls requirements. A configured destination is not proof that logs arrive.
Optional CMEK and lifecycle rules remain explicit inputs.

`create_only_workload = true` adds the NOVA training checkpoint boundary: at
least one creator must also be a viewer, objectAdmin is forbidden, versioning
is required, and lifecycle policy may only abort incomplete multipart uploads.
This Terraform guard cannot enforce request preconditions. The object client
must still use `ifGenerationMatch=0`, stream and verify checksums, upload every
rank shard, and publish the digest-bound manifest last. A generation returned
by Cloud Storage is the committed artifact identity.

The module contains no objects or secrets and does not grant the Cloud Storage
service agent access to a supplied KMS key. Establish that cross-service grant in
the key-owning state to preserve separation of duties. Retention locking requires
an exact acknowledgement because it is irreversible.

```hcl
module "artifacts" {
  source = "../../modules/storage"

  project_id          = "mindclade-development"
  name                = "mindclade-development-artifacts"
  location            = "US-CENTRAL1"
  access_log_bucket   = "mindclade-central-storage-logs"
  environment         = "development"
  owner               = "cloud-platform"
  data_classification = "restricted"
}
```

Validate with `terraform init -backend=false`, `terraform validate`, and
`terraform test`. A passing offline test is not evidence that IAM, KMS, quotas,
replication, alerting, or restore exercises are correct in a live project.

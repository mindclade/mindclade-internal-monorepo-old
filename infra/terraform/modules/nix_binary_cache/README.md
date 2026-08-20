# Nix binary-cache bucket module

This module composes `../storage` into a private, CMEK-encrypted Cloud Storage
backend for immutable Nix binary publication. Unlike the Bazel remote cache, this
bucket is not modeled as disposable performance data: a published store path and
its `.narinfo` metadata are an **immutable artifact contract** and must remain
reproducible, signed, and recoverable.

Uniform bucket-level access, enforced public-access prevention, versioning, soft
delete, a minimum retention period, server-access logging, additive IAM member
grants, `force_destroy = false`, and both Terraform and provider deletion guards
are inherited from the storage module. A required CMEK must share the bucket
location. No lifecycle rule deletes or transitions live or noncurrent objects;
only incomplete multipart uploads are aborted after seven days.
`data_classification` accepts `internal`, `confidential`, or `restricted` so
published artifacts retain an explicit governance label; the bucket remains
private in every case and public classification is forbidden.

Publishers receive `roles/storage.objectCreator` and
`roles/storage.objectViewer`, never object-admin access. Publication tooling must
also send `ifGenerationMatch=0`; IAM alone cannot make an object key immutable.
Reader workload identities and groups receive additive
`roles/storage.objectViewer`. Public, domain, direct-user, and wildcard principals
are rejected. Nix signing keys are intentionally outside Terraform state: sign NAR
metadata in a hardened build/signing service, distribute only the trusted public
key, verify signatures on every client, and publish payloads before metadata.

## Integration and operations

The separately governed logging bucket must grant
`group:cloud-storage-analytics@google.com` `roles/storage.objectCreator`; the exact
grant is returned by `required_access_log_writer_grant`. Grant the Cloud Storage
service agent `roles/cloudkms.cryptoKeyEncrypterDecrypter` on the selected key
outside this module. Authenticated HTTPS use may require client credential/header
integration; never interpret the returned substituter URI as anonymous access.

Retention lock is optional and off by default. Enabling it is irreversible and
requires the exact acknowledgement. Review legal retention, recovery, KMS
cryptoperiod and key-destruction controls, storage growth, and decommissioning
before locking. Budget for retained generations, soft delete, KMS, access logs,
requests, and egress. Monitor publication failures, generation-precondition
failures, signature verification, KMS errors, authorization failures, log delivery,
object count, bytes, and restore exercises.

Provider-mock tests prove configuration contracts and input rejection only; they
do not prove bucket-name availability, IAM/KMS propagation, signing correctness,
client authentication, artifact reproducibility, or recovery procedures.

# Bazel remote-cache bucket module

This module composes `../storage` into a private, CMEK-encrypted Cloud Storage
backend for Bazel remote caching. The cache is explicitly **rebuildable**: source
control and pinned build inputs remain authoritative, while cached action results
and content-addressed blobs only reduce build latency.

Uniform bucket-level access, enforced public-access prevention, versioning, soft
delete, a minimum retention period, server-access logging, additive IAM member
grants, `force_destroy = false`, and both Terraform and provider deletion guards
are inherited from the storage module. A required CMEK must share the bucket
location. Live entries expire after `cache_ttl_days`; old noncurrent generations
expire sooner, and incomplete multipart uploads are aborted after one day. The
retention period cannot exceed the live-entry TTL and is deliberately not locked.
`data_classification` accepts `internal`, `confidential`, or `restricted` so build
outputs retain an explicit governance label; the bucket remains private in every
case and public classification is forbidden.

Readers receive `roles/storage.objectViewer`. Cache identities in `writer_members`
receive `roles/storage.objectCreator` and viewer access, but never object-admin
access; lifecycle policy, not the executor, owns deletion. Publishers must use
content-addressed keys and `ifGenerationMatch=0` so a cache entry cannot be
overwritten. Keep the writer set limited to workload identities; no public
principal is accepted. These are additive IAM grants and do not replace unrelated
bucket IAM policy. Human reader access uses groups, never direct user or domain
grants, and wildcard principals are rejected. A cache implementation that requires
overwrites or ad hoc deletion is outside this hardened contract and needs a
separate security review.

## Integration and operations

The separately governed logging bucket must grant
`group:cloud-storage-analytics@google.com` `roles/storage.objectCreator`; the exact
grant is returned by `required_access_log_writer_grant`. Grant the Cloud Storage
service agent `roles/cloudkms.cryptoKeyEncrypterDecrypter` on the selected key
outside this module. Configure clients for authenticated HTTPS or `gs://` access,
TLS verification, bounded retries, and digest verification. Never publish cache
URLs as anonymous endpoints or treat cache hits as a provenance decision.

Budget for versioning, soft-delete, access-log, KMS, request, and egress costs.
Cloud Storage evaluates both lifecycle and retention policy, so an entry becomes
eligible at its configured TTL but deletion can be delayed until retention permits
it. Monitor cache hit ratio, latency, error rate, object count/bytes, lifecycle deletion,
KMS errors, authorization failures, and access-log delivery. Test cache loss and a
cold rebuild before production. Decommissioning requires a reviewed code change to
remove deletion guards, waiting for retention constraints, and separately handling
the CMEK and access-log records.

Provider-mock tests prove configuration contracts and input rejection only; they
do not prove bucket-name availability, cloud IAM propagation, logging delivery,
KMS permissions, cache correctness, or build reproducibility.

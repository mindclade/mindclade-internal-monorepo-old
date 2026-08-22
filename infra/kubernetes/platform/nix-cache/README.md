# Nix binary-cache server boundary

This source materializes a deliberately unusable Attic server boundary. The deployment uses the
reviewed upstream AMD64 image for commit `9eda345a743f50999de04f59a170806c3e029eea`, renders with
zero replicas, exposes no external service, has no egress, references an absent runtime Secret,
and publishes only `.invalid` client endpoints. Scaling the Deployment cannot activate it.

Attic is currently an upstream-labeled early prototype. Its managed-signing model is valuable:
publishers receive scoped JWTs while the Nix signing key remains server-side. Its S3 backend,
however, cannot use GCP Application Default Credentials. GCS interoperability requires an HMAC
key through the XML API, and Attic uses the same S3 endpoint to generate presigned downloads.
The proposed private backend therefore remains blocked on explicit credential lifecycle and
network qualification rather than being described as keyless.

An operator-owned GitOps overlay must provide all activation state: qualified endpoint/TLS and
allowed hosts, internal and public routing, reviewed SecretSync objects, PostgreSQL HA and
restore, the exact GCS bucket, narrowly scoped token claims, ingress and required egress, image
mirror/attestation, monitoring, disruption/capacity policy, and nonzero replicas. Terraform may
create only secret containers; HMAC material, database credentials, JWT signing material, and
cache tokens are written and rotated outside Terraform state.

Automatic and manual Attic garbage collection remain forbidden while the backend identity has
create-only/read IAM. Before activation, prove multipart upload cleanup, duplicate content
behavior, object overwrite and delete denial, presigned downloads, NAR signature/tamper failure,
private read denial, token expiry/revocation, database and signing-key restore, cold/warm closure
builds, cache loss, regional egress, storage growth, and rollback. Remove every client substituter
before scaling the service down.

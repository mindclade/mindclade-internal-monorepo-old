# Secret Manager module

This module creates one deletion-protected Secret Manager metadata container with
automatic or explicit multi-region replication, optional CMEK, delayed version
destruction, notifications, a rotation schedule, governance labels, and additive
least-privilege IAM. It never accepts or creates a secret payload.

Restricted secrets require CMEK in every selected replica. Payload-access and
version-adder principals must be disjoint. Rotation timestamps are bounded to the
Secret Manager API window. A rotation plan must retain the API's five-minute lead
time when applied within 24 hours, and an apply-time check rejects an expired
timestamp; the protected apply workflow must discard saved plans older than 24
hours. Annotations are restricted to printable non-sensitive metadata with API-valid
keys and a byte-exact ASCII size ceiling.

Write payload versions through an approved runtime or rotation workflow so plaintext
does not enter Terraform configuration, plans, logs, or state. Rotation scheduling
only emits a Pub/Sub notification; it requires an independently deployed, monitored,
and idempotent handler. KMS and Pub/Sub service-agent grants belong to their owning
states to preserve separation of duties.

Before rollout, validate effective IAM and inheritance, service-agent grants, data
residency, KMS location/availability, rotation and rollback, stale-version disablement,
audit-log routing, access-deny canaries, and emergency access. Offline tests prove the
metadata policy only and never access a live secret.

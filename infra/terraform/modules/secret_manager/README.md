# Secret Manager module

This module creates deletion-protected Secret Manager metadata containers with
automatic or explicit multi-region replication, optional CMEK, delayed version
destruction, notifications, a rotation schedule, governance labels, and additive
least-privilege IAM. It never accepts or creates a secret payload.

Restricted secrets require CMEK in every selected replica. Payload-access and
version-adder principals must be disjoint. `rotation_period` and
`next_rotation_time` are required together and bounded to the Secret Manager API
window. A plan-time precondition retains the API's five-minute lead time; protected
apply workflows must discard stale saved plans because the plan timestamp remains
stable during apply. Annotations are restricted to printable non-sensitive metadata with API-valid
keys and a byte-exact ASCII size ceiling.

Write payload versions through an approved runtime or rotation workflow so plaintext
does not enter Terraform configuration, plans, logs, or state. Rotation scheduling
only emits a Pub/Sub notification; it requires an independently deployed, monitored,
and idempotent handler. When the module must create the topic,
`rotation_topic_kms_key_name` is mandatory and the topic is deletion-protected;
`required_rotation_topic_kms_grant` reports the additive Pub/Sub service-agent grant.
KMS and caller-supplied topic IAM remain in their owning states to preserve separation
of duties.

Before rollout, validate effective IAM and inheritance, service-agent grants, data
residency, KMS location/availability, rotation and rollback, stale-version disablement,
audit-log routing, access-deny canaries, and emergency access. The
`unexpected_access_alert_intent` output passes the alerting requirement to the
observability state; this IAM/metadata module deliberately does not own paging.
Offline tests prove the metadata policy only and never access a live secret.

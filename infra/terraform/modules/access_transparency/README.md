# `access_transparency`

The record of Google personnel reading customer content, and the alert that makes somebody
look at it.

Both halves are here because either alone is close to useless. A bucket nobody reads is
evidence for an investigation that may never start; an alert with no durable record is a
notification somebody dismissed. Together they answer "did this happen, and was it justified"
months later, which is when the question actually gets asked.

Enabling Access Transparency itself is an organization-level entitlement tied to a support
plan. It is not a Terraform resource and is not attempted here — this module handles what
happens to the logs once the entitlement exists.

## Why each guard is there

- **The filter must mention `access_transparency`.** A filter that selects nothing produces an
  empty bucket, and an empty bucket is indistinguishable from an estate nobody accessed —
  exactly the wrong conclusion to draw silently.
- **Retention is at least a year**, enforced by a bucket retention policy rather than a
  lifecycle rule. A lifecycle rule deletes on a schedule; a retention policy stops an object
  being deleted early, *including by whoever is being investigated*.
- **An alert with no notification channels is rejected.** Google accepts one, the policy shows
  as enabled, and the first anyone knows is that a page never arrived.
- **The alert is a log-match condition, not a metric threshold.** A metric needs an aggregation
  window, and any window turns "somebody read customer data" into a count — which is the one
  framing that makes the individual justification unreadable. The rate limit is set to the
  shortest period the API permits, present to satisfy the API rather than to suppress
  anything.

`notification_channels` takes email addresses; a Monitoring channel is created for each.

The `writer_identity` output is what to check against the bucket's IAM policy if entries stop
arriving — a sink whose writer lacks permission reports healthy and writes nothing.

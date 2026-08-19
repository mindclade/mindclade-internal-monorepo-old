# `log_sink`

Organization-level log sinks and the destinations they write into.

## The failure this module is shaped around

A sink is created with a writer identity Google mints at creation time. That identity has no
permission on the destination until something grants it, and **a sink whose writer cannot
write does not error** — it reports healthy and drops every entry. The gap is visible only as
an absence nobody is looking for.

So the ordering is destination → sink → grant, with the grant depending on both, and
`unique_writer_identity` throughout: the shared default writer would give every sink the same
principal and make a per-sink grant meaningless. The `writer_identities` output is the value
to check against a destination's IAM policy when entries stop arriving.

## Two destinations

- `logging` — a Cloud Logging bucket in `project_id`. Queryable, optionally through Log
  Analytics. **`enable_analytics` can only be set at creation**; turning it on later requires
  deleting the bucket, which takes every entry in it.
- `storage` — a GCS bucket. Versioned, uniform access, public access prevented, optional CMEK.

Retention on a GCS destination is a **retention policy**, not a lifecycle delete rule: a
lifecycle rule removes objects on a schedule, a retention policy stops them being removed
early. It is deliberately never locked here — a locked policy cannot be shortened by anyone,
ever, including to correct a mistake, so locking stays a separate operational act.

`default_sink_retention_days` shortens each project's own `_Default` bucket, which is what
stops every project paying to store logs this module already keeps centrally. Set it to `0`
to leave `_Default` alone; `0` means untouched, not zero-day retention.

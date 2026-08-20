# Operator observability

This package owns the Google Managed Service for Prometheus scrape and recording-rule contracts
for the pinned Kueue and JobSet controllers. It deliberately does not install Prometheus,
Prometheus Operator, Alertmanager, or the cluster-wide `gmp-public/config` `OperatorConfig`.

Both monitors declare the controllers' authenticated TLS endpoints. They reference externally
managed, rotating bearer-token and CA-only trust Secrets; the JobSet endpoint also references an
externally managed client certificate with `clientAuth` usage. The Kubernetes manifests never
contain tokens, certificate material, or Secret objects, and the collector is not given access to
the controller serving Secrets that contain private keys. Chart `ServiceMonitor` generation stays
disabled.

## Activation blockers

The GitOps Application for this package remains paused until a qualification cluster proves:

- the rendered selectors resolve only the intended controller Pods and their named `metrics` port;
- the bearer tokens are short-lived TokenRequest credentials for exact scraper ServiceAccounts,
  reload on rotation, and are bound only to the upstream `*/metrics` reader ClusterRoles;
- the collector can read only the named external credential/trust Secrets in each target
  namespace, while the JobSet client certificate has `clientAuth` usage and a verified chain;
- `ConfigurationCreateSuccess=True`, all expected targets are active, no target is unhealthy, and
  target status is fresh;
- an environment overlay permits ingress from the exact managed collector identity to TCP 8443;
- the rule evaluator can query the recorded series without dropping protected GMP labels; and
- Cloud Monitoring owns paging, notification channels, and SLO burn policies.

No collector namespace or Pod labels are guessed in this base. The live-network allow policy is an
environment-owned promotion input, and the namespace default-deny policy remains effective until
that reviewed input exists.

Scrape relabeling keeps only controller/runtime health and Kueue queue/admission metric families.
JobSet's upstream terminal counters are deliberately excluded: they are partitioned by unique
`jobset_name`, first appear at one during the terminal transition, and then remain at one. A range
`increase()` therefore does not reliably observe an ordinary JobSet's only completion or failure,
and retrying a conflicted status update can increment the counter again. Capacity remains blocked
until a durable condition/event exporter supplies bounded JobSet outcome signals.

Recording rules retain only cluster, location, namespace, queue, result, and queue-status labels.
Reconcile error ratios use counter increases and publish matching event counts so alert policies
can enforce minimum traffic without understating low-volume failures. Tenant, model, dataset,
prompt, feature, label, request, and JobSet-name identifiers remain forbidden from recorded alert
series.

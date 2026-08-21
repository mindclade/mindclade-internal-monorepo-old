# Control-plane Kubernetes source contracts

All control-plane Deployments remain disabled with non-routable images. The API and maintenance
Pods declare the bounded `metrics` port and telemetry selector because both roles own dedicated
private listeners. `PodMonitoring/control-admission` keeps only the admission metric families and
maps the fixed process role into the bounded `role` target label. Recording rules remove Pod
identity before a signal is consumed by an alert or dashboard. They also require the fixed
`service=control-admission` target label and compare discovered targets with the desired Deployment
replicas, so another API target, one failed scrape, or one missing Pod cannot be hidden by a
healthy replica.

API counters and histogram buckets sum across replicas. Exact versioned series-inventory rules
also require every desired API and maintenance replica to expose its complete fixed contract.
Maintenance backlog, oldest age, drift,
and consecutive-backlog recordings take the conservative maximum. Sweep and probe age derive from
the minimum success timestamp, and snapshot success takes the minimum across replicas. Every
maintenance replica runs both bounded PostgreSQL probes; a failed, stale, or partially
instrumented replica cannot be hidden by a healthy peer.

The base deliberately grants no ingress to TCP 9464. Namespace-wide default deny remains the
authority until an environment overlay supplies a NetworkPolicy whose source selects the exact
managed collector identity observed in the qualification cluster. Do not substitute a guessed
namespace, ServiceAccount, or mutable generic label. The overlay must be reviewed again whenever
the managed collector identity changes.

Activation additionally requires the API Pod template to expose the named port, bounded scrape
samples and labels, healthy and fresh GMP target/rule status, representative decision volume,
synthetic fire/resolve evidence, and delivery through non-production notification channels.
Source rendering and promtool results are not connected qualification.

The admission API owns the decision counter and latency histogram. Maintenance owns a private
registry/listener plus background bounded snapshots for expiration backlog, sweep freshness, and
audit/outbox drift. The maintenance `Rules` resource is separate from the API decision rules and
remains explicitly activation-blocked until v14 migration receipts, representative PostgreSQL
volume, GMP collection, alert translation, and fire/resolve behavior are connected-qualified.

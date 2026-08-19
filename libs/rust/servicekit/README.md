# `mindclade_servicekit`

Mandatory Rust process-lifecycle substrate: explicit health/readiness, Created → Starting → Running → Draining → Stopping → Stopped lifecycle, reverse-order component drain/stop, cooperative shutdown, and named task supervision. Business policy, network stacks, and scientific semantics stay outside this crate.

## Adoption

| Crate | Uses |
| --- | --- |
| `services/runtime_gateway` | `Service`, `Component`, `signals` |
| `services/runtime_host` | `Service`, `Component`, `signals` |
| `services/artifact_proxy` | `Component` |
| `services/node_agent` | `Component` |

`Supervisor` and `HealthRegistry` have no consumer yet, and neither is a
drop-in for the services above: `Supervisor` supervises OS threads while every
deployable Rust service here is Tokio-task based, and both runtime services
already own richer domain health types (`GatewayHealth`, `HostHealth`) that
carry more than a status enum. Adopting either means changing what it is, not
just calling it.

## Signals

`signals::termination_requested()` waits for **SIGTERM or SIGINT**, and
`SignalHandle::install()` bridges that to a `ShutdownToken`.

SIGTERM is the one that matters: Kubernetes, Cloud Run, and `docker stop` all
send it and none send SIGINT, so a process waiting only on `ctrl_c` never
drains — it idles through the termination grace period and is then killed with
SIGKILL, mid-request. Registration failure deliberately never resolves, because
callers read a resolve as "shutdown requested" and a spurious one would stop the
process during startup.

This is why the crate depends on `tokio`. `std` has no OS-signal API and the
`libc` route needs `unsafe`, which this workspace denies. Only the `signal`
feature is used, but the dependency reaches the sync-only consumers
(`artifact_proxy`, `node_agent`) transitively.

# `mindclade_servicekit`

Mandatory Rust process-lifecycle substrate: explicit health/readiness, Created → Starting → Running → Draining → Stopping → Stopped lifecycle, bounded reverse-order component drain/stop, cooperative shutdown, and named task supervision. Business policy, network stacks, and scientific semantics stay outside this crate.

## Phase graph

The phases and their transitions are shared with the Go control plane
(`libs/go/servicekit/state.go`, `State.CanTransitionTo`), because a node's phase
only means something to a fleet controller if both runtimes name the same phases
and admit the same transitions:

```text
new -> starting -> running -> draining -> stopping -> stopped
        │            │           │            │
        │            └───────────┴────────────┴──> failed
        ├──> draining   (termination requested before running is announced)
        └──> stopping   (startup failure)
```

`LifecycleState::as_str` reports the Go runtime's `service_state` vocabulary
verbatim — `Created` is `new` — so one phase token means one thing across both
languages. `tests/lifecycle.rs` pins the matrix, the names, and the probe
predicates edge by edge; `libs/go/servicekit/lifecycle_contract_test.go` pins
the same table on the Go side.

## Readiness and liveness

Answer probes with `server::ready` and `server::live`, which are the conjunction
of phase and dependency health. `HealthRegistry::is_ready` on its own is
dependency health: it cannot see the phase, so a draining process whose
dependencies are all healthy still reports ready and keeps receiving new traffic
until its listener dies. `HealthRegistry::is_live` likewise cannot see a failed
lifecycle, because a failed process leaves no unhealthy dependency report
behind.

## Shutdown budgets

`ServiceConfig::drain_timeout` bounds the reverse-order drain pass and
`shutdown_timeout` bounds the whole of `Service::stop`, drain included.
`Service::new` uses the same defaults as the Go runtime (10s drain, 30s total).
An unbounded shutdown is not a slow shutdown: the orchestrator's grace period
ends and the process is killed with SIGKILL mid-request, which is exactly what
the drain exists to avoid.

The two budgets are separate so that an overspent drain still leaves the stop
pass something to spend — otherwise a slow drain means no component is ever
stopped. `validate` rejects a `drain_timeout` that is not strictly smaller than
`shutdown_timeout` for that reason. A drain requested on its own, the way a
composition root closes admission the moment SIGTERM arrives, spends its own
budget; the later `stop` then spends the shutdown budget from when it is
called, and the interval between them is the process serving out established
work.

The bound is a budget for the pass, not preemption of a single hook. Hooks are
synchronous `&mut self` calls and cannot be cancelled from the coordinator, so a
hook that blocks forever still blocks the process — hooks must return. Go
bounds each hook individually because it can abandon a goroutine; that is the
one place the two runtimes differ, and it is a property of the languages rather
than a difference in policy.

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

# Fenced leadership

`leadership` adapts `storage/lease` into a service-managed singleton election.
The leader handler receives a session containing the current fenced lease and
is cancelled when renewal fails.

```go
elector, err := leadership.New(leaseStore, leadership.Config{
    Key:   key,
    Owner: instanceID,
    TTL:   30 * time.Second,
}, func(leaderCtx context.Context, session leadership.Session) error {
    return runController(leaderCtx, session.Lease.FencingToken())
})

builder.AddCapability(
    production.CapabilityLeadership,
    elector.Component("scheduler-leadership"),
)
```

`GateComponent` moves one component's `Run` under the elector.
`GateComponents` does the same for several that must lead or stop together: it
returns them keyed by name, refuses a duplicate name, and cancels the whole
group when any member stops, because a role half-running under a lease it still
holds is worse than a role that surrendered it.

Domain state commits must include/check the fence when stale ownership would be
unsafe. Do not launch a process-local election goroutine outside servicekit.

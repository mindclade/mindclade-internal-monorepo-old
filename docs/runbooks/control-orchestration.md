# control.orchestration runbook

## Trigger

A workflow stops advancing, an attempt is stuck or duplicated, a worker reports a status or
uploads an artifact that the control plane rejects, or a lease/fencing anomaly is alerted.

## Immediate actions

1. Record the identity of the affected work: run ID, job ID, stage ID, attempt number, the
   current fencing token, lease age, last heartbeat time, and claim state. Capture these before
   changing anything; they are the evidence that distinguishes a stalled worker from a stale one.
2. Verify the worker still holds the current claim before accepting any status transition or
   artifact commit from it. A claim that has been reclaimed is not a slow claim.
3. If the claim is expired or stale, allow fenced reclamation to proceed and reject the late
   completion. Rejecting late work is the correct outcome, not an incident to suppress.
4. Do not hand-edit stage or attempt state to "unstick" a workflow. Terminal transitions are
   forward-only, and a manual reopen is indistinguishable from the corruption this component
   exists to prevent.
5. Preserve bounded diagnostics and audit evidence for the affected attempts.

## Recovery

Let the executor requeue the stage under a newer fencing token once lease state permits it.
Never reuse a stale execution ticket during recovery: a reclaimed attempt gets a new ticket and a
new token, and the old pair must stay invalid. Confirm the attempt budget accounting reflects the
reclamation — a fenced reclamation consumes an attempt, and a workflow that has exhausted its
budget fails rather than looping. Cancellation propagation resumes from the recorded intent held
by `control/runs`; orchestration re-propagates it, it does not re-decide it.

## Exit criteria

No late worker can commit, every stage has exactly one live claim, attempt counts match the
declared budget, workflow definition digests still match the compiled plan, terminal states have
not moved backwards, and the stall duration is recorded against the service SLO.

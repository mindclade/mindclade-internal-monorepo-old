# Runbook: control evidence

Serves the `control/evidence` package and its single production consumer, the control-plane
eligibility runtime (`services/control_plane/internal/providers/api/eligibility_runtime.go`).

## Trigger

Every eligibility call fails with `evidence_policy_expired` or `evidence_service_unconfigured`;
release decisions cannot be evaluated, signed, or read; or evidence submission is rejected while the
process is otherwise healthy.

## Hazard 1 — an expired policy takes down the whole domain, including reads

This is the largest operational hazard in this component. Read it before triaging anything else.

All six exported methods route through one guard: `Submit`
(`control/evidence/service.go:45`), `Record` (`:88`), `Evidence` (`:112`), `Evaluate` (`:122`),
`GetDecision` (`:200`), and `Revoke` (`:207`) each call `service.validate`. That guard returns
`evidence_policy_expired` as soon as `Policy.ValidUntil` is not after the current time
(`control/evidence/service.go:226-227`).

The policy is read once, from a file, at construction
(`services/control_plane/internal/providers/api/eligibility_runtime.go:36`), and startup already
refuses a policy that is expired at that moment (`:53`). So the failure mode is **expiry after a
successful start**. When that happens:

- the process stays up and healthy from the orchestrator's point of view;
- every call fails, including pure reads — `Evidence` and `GetDecision` are not exempt;
- `Revoke` fails too, so the domain cannot even be used to withdraw a decision;
- nothing self-heals, because the policy is never re-read.

Recovery is: ship a new policy file with a later `ValidUntil`, then **restart the process**. There is
no reload path. Treat approaching `Policy.ValidUntil` as a scheduled operational event with lead
time, not as an alert to react to — by the time it fires, the domain is already down.

Do not attempt to restore availability by relaxing the expiry check. The guard is fail-closed on
purpose: an expired policy means the control set and its owners are no longer known to be current,
and signing decisions against a stale policy is worse than refusing to sign.

## Hazard 2 — `Submit` is unreachable in production

`Submit` requires a non-nil `Verifier` (`control/evidence/service.go:45` passes
`requireVerifier=true`, enforced at `:220`). Production wiring constructs the service with four
fields and no verifier:

```go
evidence.Service{Repository: repository, Policy: policy, Signer: signer, Clock: value}
```

(`services/control_plane/internal/providers/api/eligibility_runtime.go:63`). Every production
`Submit` call therefore returns `evidence_service_unconfigured`.

If `Submit` is what an operator is reaching for, it will not work and no configuration change fixes
it — a `Verifier` implementation has to be constructed and wired. `Record` (`:88`) does not require
a verifier and is the reachable path for evidence produced independently; it binds the claim to the
active policy rather than trusting the caller (`:99-106`).

## Triage

1. Read the returned fault reason. It is the diagnosis:
   - `evidence_policy_expired` — hazard 1 above.
   - `evidence_service_unconfigured` — a nil `Repository`, `Clock`, `Verifier`, or `Signer` for the
     method called (`control/evidence/service.go:220-221`). For `Submit` in production this is
     expected, not an incident.
   - `evidence_control_unknown` (`:50`, `:99`) — the claim names a control absent from the active
     policy; the policy and the caller disagree about the control set.
   - `evidence_policy_binding_invalid` (`:103`) — claim owner, claim digest, policy digest, or
     policy epoch does not match the active policy. Usually a policy rollout that reached some
     producers and not others.
   - `evidence_validity_exceeds_policy` (`:105-106`) — the submitted validity outlives the claim or
     the control's `MaximumAge`.
2. Confirm which policy file and epoch the running process loaded. It is whatever was on disk at
   start, which is not necessarily what is on disk now.
3. Check `Policy.ValidUntil` against wall clock before investigating anything else.

## Recovery

- **Expired policy:** publish a policy with a later `ValidUntil`, restart, confirm reads recover.
  Do not backdate the clock and do not patch out the guard.
- **Policy-binding mismatch after a rollout:** finish the rollout so producers and the evidence
  service agree on digest and epoch, rather than accepting unbound evidence.
- **Decision validity questions:** note that `Evaluate` clamps `ExpiresAt` down to the earliest of
  the policy, claim, and verification validity (`control/evidence/service.go:138-139,162-167`), and
  requires every control to pass and be unexpired (`:168`). A decision that expires sooner than
  expected is that clamp working, not a defect.
- **Wrong or withdrawn decision:** use `Revoke` (`:207`) — which also needs a live policy.

## Exit criteria

The active policy is unexpired and its epoch matches producers, reads and evaluations succeed, the
cause of expiry-without-warning has a scheduled-renewal follow-up, and no guard was weakened to
restore service.

## Known limitations recorded here deliberately

- No metrics, logs, or traces are emitted by `control/evidence`. Every signal above is a fault
  reason observed by the caller.
- Policy is load-once. There is no reload, no re-read, and no expiry warning.

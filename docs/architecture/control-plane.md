# Go control plane

The control plane is the durable policy authority. It is a modular monolith at
the code and persistence boundary with independently deployable process roles.

## Domain modules

```text
control/
  admission       quota and entitlement admission
  artifacts       artifact metadata and access policy
  audit           audit policy and export coordination
  evaluations     evaluation records and gate state
  events          event catalog mappings and publication policy
  evidence        immutable claims, verification, signed eligibility
  ingestion       source snapshots and ingestion stage state
  lineage         provenance graph and relations
  metadata        run/build/metric metadata
  orchestration   durable workflow/job state machines
  registry        datasets, models, checkpoints, references, releases
  routing         deployment and route-snapshot policy
  runs            run/job/attempt lifecycle
  runtime_authority admission grants, execution tickets, revocation epochs
  scheduling      global queues, fair share, placement, reservations
  tenancy         organizations, projects, identities, entitlements
  usage           metering and quota reconciliation
  webhooks        delivery policy and subscriptions
  weights         model-weight access approvals and receipts
```

Reusable mechanisms remain in `libs/go`; generated wire types remain in
`protocols/`; deployable wiring remains in `services/control_plane/`.

The list above is the blueprint boundary set. It does not assert that each
directory is implemented: `audit`, `evaluations`, `events`, `metadata`, `runs`,
`scheduling`, `tenancy`, `usage`, `webhooks`, and `weights` currently hold only
reserved package boundaries and are undeclared in `components.toml`. See
[`system-design-reference.md` §7.1](system-design-reference.md) for the current
split and `components.toml` for the authoritative per-component status.

## Canonical durable mutation

```text
authenticate and authorize
  -> validate idempotency identity/fingerprint
  -> begin SQL transaction
       -> apply domain mutation with resource-version precondition
       -> append immutable audit event
       -> append transactional outbox envelope
  -> commit
  -> return stable result
```

The outbox dispatcher publishes after commit. No request transaction directly
calls a broker.

## Canonical event projection

```text
broker delivery
  -> inbox/idempotency transaction
       -> apply projector effects
       -> compare-and-advance cursor
       -> optionally append downstream outbox event
  -> commit
  -> acknowledge delivery
```

## Canonical background work

Schedulers, controllers, operators, ingestion coordinators, and maintenance
processes use the shared fenced work queue: claim, heartbeat, claim-loss
cancellation, complete/retry/dead-letter. Singleton authority uses the shared
lease and leadership mechanisms.

## Runtime authority

The control plane publishes immutable signed route snapshots and bounded
admission/execution grants. Rust validates them locally. Claims include tenant,
workspace, model/runtime bundle digests, artifact scopes, resource budgets,
deadline, policy/route versions, fencing token, issue/expiry time, signature,
and key ID.

## Outage semantics

- already-admitted work continues within its ticket and deadline;
- valid, unexpired grants may continue within remaining local budget;
- new work without a valid grant is rejected;
- expired route or revocation state causes fail-closed drain;
- usage is durably spooled within a bound; when the bound is exhausted, new
  work is rejected rather than becoming unaccounted;
- a newer revocation epoch invalidates cached grants/routes immediately.

## Process roles

The standard bootstrap supports API, scheduler, controller, operator,
ingestion coordinator, projector, event dispatcher, webhook dispatcher,
registry, admin, and maintenance profiles. Each profile declares exact required
capabilities and refuses startup when a required provider or loop is absent.

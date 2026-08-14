# Tenant and workspace isolation

## Authority

Go owns tenancy, membership, service accounts, entitlements, quotas, policy,
audit, and global scheduling. Rust enforces locally verifiable tenant-scoped
grants and artifact prefixes. Python receives only the inputs and output scope
for the admitted job/request.

## Required controls

- Canonical tenant/workspace IDs in every durable resource, event, ticket,
  artifact reference, usage record, and audit event.
- Authentication and authorization before every external mutation/read.
- Database queries bind tenant scope independently of caller-provided filters.
- Signed online grants and execution tickets include tenant/workspace, allowed
  deployment/capability, artifact scopes, budgets, expiry, epochs, and fencing.
- Artifact proxy and object-store wrappers enforce namespace/grant scope before
  byte access.
- Cache keys are tenant-bound unless the data is explicitly safe and immutable
  for cross-tenant sharing.
- Kubernetes workload identity and namespace/network policy limit provider and
  service access.
- Usage and quota reconciliation is durable; bounded local usage authority
  fails closed when exhausted.

## Tests

Cross-tenant negative tests cover IDs, pagination cursors, artifact ranges,
signed URLs, webhooks, events, route selection, work queues, caches, logs, and
administrative APIs. Release qualification includes attempts to reuse valid
objects/tickets in the wrong tenant or workspace.

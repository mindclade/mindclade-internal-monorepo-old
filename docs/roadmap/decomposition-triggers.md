# Service decomposition triggers

The Go control plane begins as a modular monolith. A module becomes an
independent service only when one or more measured triggers justify the
operational cost.

## Valid triggers

1. **Independent scale:** workload or throughput cannot be handled efficiently
   within the shared deployment.
2. **Availability objective:** a module needs a materially different SLO or
   fault domain.
3. **Security boundary:** credentials, data classification, or tenant isolation
   require process/network separation.
4. **Operational ownership:** a stable team owns on-call, releases, capacity,
   and incident response independently.
5. **Release cadence:** independent deployment materially reduces risk or
   unlocks necessary iteration.
6. **Resource profile:** CPU, memory, network, storage, or runtime requirements
   conflict with the parent process.
7. **Data contention:** proven transactional/database contention cannot be
   solved with schema/index/query/partition improvements.
8. **Regulatory boundary:** evidence, retention, or regional constraints require
   isolated operation.

## Required extraction evidence

Before splitting, document API/event contracts, ownership of source-of-truth
state, migration strategy, transaction replacement, failure/outage semantics,
backfill/replay, SLOs, capacity, security, observability, deployment, rollback,
and total operational cost.

“Microservice symmetry,” directory aesthetics, and hypothetical future scale are
not valid triggers.
